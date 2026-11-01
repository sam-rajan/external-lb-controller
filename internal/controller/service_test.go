/*
Copyright 2026 The out-tree-loadbalancer-controller Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/sam-rajan/out-tree-loadbalancer-controller/internal/lb"
)

// These tests use a fake API server client and a fake lb.Provider, so unlike
// the Ginkgo suite in suite_test.go they need neither envtest binaries nor
// cloud credentials.

type fakeProvider struct {
	hostname  string
	ensureErr error
	deleteErr error

	ensureCalls int
	deleteCalls int
	gotSlices   []discoveryv1.EndpointSlice
}

func (f *fakeProvider) EnsureLB(_ context.Context, _ *corev1.Service, eps []discoveryv1.EndpointSlice) (string, error) {
	f.ensureCalls++
	f.gotSlices = eps
	return f.hostname, f.ensureErr
}

func (f *fakeProvider) DeleteLB(context.Context, *corev1.Service) error {
	f.deleteCalls++
	return f.deleteErr
}

var fakeProviderCounter int

// newReconciler wires a ServiceReconciler to a fake client holding objs and
// to fp, registered through the public lb registry.
func newReconciler(t *testing.T, fp *fakeProvider, objs ...client.Object) (*ServiceReconciler, client.Client, *events.FakeRecorder) {
	t.Helper()

	c := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(objs...).
		WithStatusSubresource(&corev1.Service{}).
		Build()

	fakeProviderCounter++
	name := fmt.Sprintf("fake-%d", fakeProviderCounter)
	lb.Register(name, func(context.Context, lb.ProviderConfig) (lb.Provider, bool) { return fp, true })
	mgr, err := lb.NewManager(context.Background(), lb.ProviderConfig{CloudProvider: name})
	if err != nil {
		t.Fatal(err)
	}

	rec := events.NewFakeRecorder(10)
	return &ServiceReconciler{Client: c, Scheme: runtime.NewScheme(), LBManager: mgr, Recorder: rec}, c, rec
}

func ourService(mutate func(*corev1.Service)) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "web"},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: new(lbClass),
			Ports:             []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
		},
	}
	if mutate != nil {
		mutate(svc)
	}
	return svc
}

func doReconcile(r *ServiceReconciler) (ctrl.Result, error) {
	return r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "web"}})
}

func getService(t *testing.T, c client.Client) *corev1.Service {
	t.Helper()
	svc := &corev1.Service{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "web"}, svc); err != nil {
		t.Fatal(err)
	}
	return svc
}

func hasFinalizer(svc *corev1.Service) bool {
	return slices.Contains(svc.Finalizers, lbFinalizer)
}

func TestReconcileMissingServiceIsIgnored(t *testing.T) {
	fp := &fakeProvider{}
	r, _, _ := newReconciler(t, fp)
	if _, err := doReconcile(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.ensureCalls+fp.deleteCalls != 0 {
		t.Error("provider was called for a missing Service")
	}
}

func TestReconcileIgnoresForeignServices(t *testing.T) {
	foreign := map[string]func(*corev1.Service){
		"no class":      func(s *corev1.Service) { s.Spec.LoadBalancerClass = nil },
		"other class":   func(s *corev1.Service) { s.Spec.LoadBalancerClass = new("example.com/other") },
		"not a LB type": func(s *corev1.Service) { s.Spec.Type = corev1.ServiceTypeClusterIP },
	}
	for name, mutate := range foreign {
		t.Run(name, func(t *testing.T) {
			fp := &fakeProvider{hostname: "lb.example.com"}
			r, c, _ := newReconciler(t, fp, ourService(mutate))
			if _, err := doReconcile(r); err != nil {
				t.Fatal(err)
			}
			svc := getService(t, c)
			if fp.ensureCalls != 0 || hasFinalizer(svc) || len(svc.Status.LoadBalancer.Ingress) != 0 {
				t.Errorf("foreign Service was touched: calls=%d finalizers=%v", fp.ensureCalls, svc.Finalizers)
			}
		})
	}
}

func TestReconcileUpsert(t *testing.T) {
	ready := true
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns", Name: "web-abc",
			Labels: map[string]string{discoveryv1.LabelServiceName: "web"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}}},
	}
	otherSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns", Name: "other-abc",
			Labels: map[string]string{discoveryv1.LabelServiceName: "other"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}

	fp := &fakeProvider{hostname: "lb.example.com"}
	r, c, _ := newReconciler(t, fp, ourService(nil), slice, otherSlice)

	if _, err := doReconcile(r); err != nil {
		t.Fatal(err)
	}

	svc := getService(t, c)
	if !hasFinalizer(svc) {
		t.Error("finalizer was not added")
	}
	if got := svc.Status.LoadBalancer.Ingress; len(got) != 1 || got[0].Hostname != "lb.example.com" {
		t.Errorf("status ingress = %v, want hostname lb.example.com", got)
	}
	if fp.ensureCalls != 1 {
		t.Errorf("EnsureLB called %d times, want 1", fp.ensureCalls)
	}
	if len(fp.gotSlices) != 1 || fp.gotSlices[0].Name != "web-abc" {
		t.Errorf("provider got slices %v, want only web-abc", fp.gotSlices)
	}

	// Idempotent: a second reconcile keeps a single finalizer and the same status.
	if _, err := doReconcile(r); err != nil {
		t.Fatal(err)
	}
	svc = getService(t, c)
	if len(svc.Finalizers) != 1 {
		t.Errorf("finalizers after second reconcile = %v", svc.Finalizers)
	}
}

func TestReconcileUpsertProviderError(t *testing.T) {
	fp := &fakeProvider{ensureErr: errors.New("throttled")}
	r, c, _ := newReconciler(t, fp, ourService(nil))

	if _, err := doReconcile(r); err == nil {
		t.Fatal("provider error was swallowed; the request would not be retried")
	}
	svc := getService(t, c)
	if !hasFinalizer(svc) {
		t.Error("finalizer must be added before touching the cloud, so cleanup is guaranteed")
	}
	if len(svc.Status.LoadBalancer.Ingress) != 0 {
		t.Error("status must not be set when provisioning failed")
	}
}

func TestReconcileEmptyHostnameLeavesStatusAlone(t *testing.T) {
	fp := &fakeProvider{}
	r, c, _ := newReconciler(t, fp, ourService(nil))
	if _, err := doReconcile(r); err != nil {
		t.Fatal(err)
	}
	if got := getService(t, c).Status.LoadBalancer.Ingress; len(got) != 0 {
		t.Errorf("status ingress = %v, want none", got)
	}
}

func deleting(s *corev1.Service) {
	s.Finalizers = []string{lbFinalizer}
	now := metav1.Now()
	s.DeletionTimestamp = &now
}

func TestReconcileDelete(t *testing.T) {
	fp := &fakeProvider{}
	r, c, _ := newReconciler(t, fp, ourService(deleting))

	if _, err := doReconcile(r); err != nil {
		t.Fatal(err)
	}
	if fp.deleteCalls != 1 {
		t.Errorf("DeleteLB called %d times, want 1", fp.deleteCalls)
	}
	// The fake client removes an object once its last finalizer is gone.
	svc := &corev1.Service{}
	err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "web"}, svc)
	if client.IgnoreNotFound(err) != nil {
		t.Fatal(err)
	}
	if err == nil && hasFinalizer(svc) {
		t.Error("finalizer was not removed after a successful delete")
	}
}

func TestReconcileDeleteFailureKeepsFinalizer(t *testing.T) {
	fp := &fakeProvider{deleteErr: errors.New("access denied")}
	r, c, _ := newReconciler(t, fp, ourService(deleting))

	if _, err := doReconcile(r); err == nil {
		t.Fatal("delete error was swallowed")
	}
	if !hasFinalizer(getService(t, c)) {
		t.Error("finalizer was removed even though the cloud load balancer was not deleted")
	}
}

func TestReconcileDeleteSkipCleanupAnnotation(t *testing.T) {
	fp := &fakeProvider{deleteErr: errors.New("would fail")}
	r, c, rec := newReconciler(t, fp, ourService(func(s *corev1.Service) {
		deleting(s)
		s.Annotations = map[string]string{annotationSkipLBCleanup: "true"}
	}))

	if _, err := doReconcile(r); err != nil {
		t.Fatal(err)
	}
	if fp.deleteCalls != 0 {
		t.Error("DeleteLB must not be called when cleanup is skipped")
	}
	svc := &corev1.Service{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "web"}, svc); err == nil && hasFinalizer(svc) {
		t.Error("finalizer was not removed")
	}
	select {
	case e := <-rec.Events:
		if e == "" {
			t.Error("empty event")
		}
	default:
		t.Error("expected a LoadBalancerCleanupSkipped warning event")
	}
}

func TestReconcileDeleteWithoutOurFinalizerIsNoop(t *testing.T) {
	fp := &fakeProvider{}
	r, _, _ := newReconciler(t, fp, ourService(func(s *corev1.Service) {
		s.Finalizers = []string{"example.com/other"}
		now := metav1.Now()
		s.DeletionTimestamp = &now
	}))
	if _, err := doReconcile(r); err != nil {
		t.Fatal(err)
	}
	if fp.deleteCalls != 0 {
		t.Error("DeleteLB called for a Service without our finalizer")
	}
}
