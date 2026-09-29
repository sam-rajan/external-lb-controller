/*
Copyright 2026.

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
	"slices"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sam-rajan/out-tree-loadbalancer-controller/internal/lb"
)

const (
	lbClass     = "otlbc.io/lb"
	lbFinalizer = "otlbc.io/lb-cleanup"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	LBManager *lb.Manager
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=services/finalizers,verbs=update

// +kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch

func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	svc := &corev1.Service{}
	if err := r.Get(ctx, req.NamespacedName, svc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Ensure that the service has the lbClassname and hence owned by us
	if !r.ownedByUs(svc) {
		logf.FromContext(ctx).V(1).Info("Service is not owned by otlbc", svc.Name)
		return ctrl.Result{}, nil
	}

	if !svc.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, svc)
	}

	return r.reconcileUpsert(ctx, svc)
}

func (r *ServiceReconciler) reconcileUpsert(ctx context.Context, svc *corev1.Service) (ctrl.Result, error) {
	// Add finalizer so we can get the responsibility of the deletion.
	patch := client.MergeFrom(svc.DeepCopy())
	if controllerutil.AddFinalizer(svc, lbFinalizer) {
		if err := r.Patch(ctx, svc, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	logf.FromContext(ctx).Info("Reconciling load balancer for service", "service", types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace})
	epSlices, err := r.endpointSlicesForService(ctx, svc)
	if err != nil {
		return ctrl.Result{}, err
	}

	hostname, err := r.LBManager.EnsureLB(ctx, svc, epSlices)
	if err != nil {
		logf.FromContext(ctx).Error(err, "Failed to ensure load balancer")
		return ctrl.Result{}, err
	}

	if hostname != "" {
		return r.setIngressStatus(ctx, svc, hostname)
	}

	return ctrl.Result{}, nil
}

func (r *ServiceReconciler) endpointSlicesForService(ctx context.Context, svc *corev1.Service) ([]discoveryv1.EndpointSlice, error) {
	list := &discoveryv1.EndpointSliceList{}
	err := r.List(ctx, list,
		client.InNamespace(svc.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: svc.Name},
	)
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (r *ServiceReconciler) reconcileDelete(ctx context.Context, svc *corev1.Service) (ctrl.Result, error) {
	if !slices.Contains(svc.Finalizers, lbFinalizer) {
		return ctrl.Result{}, nil
	}

	logf.FromContext(ctx).Info("Deleting load balancer for service", "service", types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace})
	if err := r.LBManager.DeleteLB(ctx, svc); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to delete load balancer")
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(svc.DeepCopy())
	controllerutil.RemoveFinalizer(svc, lbFinalizer)
	return ctrl.Result{}, r.Patch(ctx, svc, patch)
}

func (r *ServiceReconciler) setIngressStatus(ctx context.Context, svc *corev1.Service, hostname string) (ctrl.Result, error) {
	patch := client.MergeFrom(svc.DeepCopy())
	svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{Hostname: hostname}}
	return ctrl.Result{}, r.Status().Patch(ctx, svc, patch)
}

func (r *ServiceReconciler) ownedByUs(svc *corev1.Service) bool {
	return svc.Spec.Type == corev1.ServiceTypeLoadBalancer &&
		svc.Spec.LoadBalancerClass != nil &&
		*svc.Spec.LoadBalancerClass == lbClass
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	eventFilter := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			svc, ok := e.Object.(*corev1.Service)
			return ok && svc.Spec.Type == corev1.ServiceTypeLoadBalancer &&
				svc.Spec.LoadBalancerClass != nil &&
				*svc.Spec.LoadBalancerClass == lbClass
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldSvc, okOld := e.ObjectOld.(*corev1.Service)
			newSvc, okNew := e.ObjectNew.(*corev1.Service)
			if !okOld || !okNew {
				return false
			}

			return (newSvc.Spec.Type == corev1.ServiceTypeLoadBalancer && newSvc.Spec.LoadBalancerClass != nil &&
				*newSvc.Spec.LoadBalancerClass == lbClass) ||
				(oldSvc.Spec.Type == corev1.ServiceTypeLoadBalancer && oldSvc.Spec.LoadBalancerClass != nil &&
					*oldSvc.Spec.LoadBalancerClass == lbClass)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			svc, ok := e.Object.(*corev1.Service)
			return ok && svc.Spec.Type == corev1.ServiceTypeLoadBalancer && svc.Spec.LoadBalancerClass != nil &&
				*svc.Spec.LoadBalancerClass == lbClass
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}, builder.WithPredicates(eventFilter)).
		Watches(
			&discoveryv1.EndpointSlice{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				svcName, ok := obj.GetLabels()[discoveryv1.LabelServiceName]
				if !ok {
					return nil
				}
				return []reconcile.Request{
					{NamespacedName: types.NamespacedName{Name: svcName, Namespace: obj.GetNamespace()}},
				}
			}),
		).
		Named("loadbalancer-service").
		Complete(r)
}
