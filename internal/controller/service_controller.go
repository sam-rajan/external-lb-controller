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

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=services/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Service object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/reconcile
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	logger.Info("THis is executed")

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	eventFilter := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			svc, ok := e.Object.(*corev1.Service)
			return ok && svc.Spec.Type == corev1.ServiceTypeLoadBalancer &&
				svc.Spec.LoadBalancerClass != nil &&
				*svc.Spec.LoadBalancerClass == "out-tree-lbc"
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldSvc, okOld := e.ObjectOld.(*corev1.Service)
			newSvc, okNew := e.ObjectNew.(*corev1.Service)
			if !okOld || !okNew {
				return false
			}

			return (newSvc.Spec.Type == corev1.ServiceTypeLoadBalancer && newSvc.Spec.LoadBalancerClass != nil &&
				*newSvc.Spec.LoadBalancerClass == "out-tree-lbc") ||
				(oldSvc.Spec.Type == corev1.ServiceTypeLoadBalancer && oldSvc.Spec.LoadBalancerClass != nil &&
					*oldSvc.Spec.LoadBalancerClass == "out-tree-lbc")
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			svc, ok := e.Object.(*corev1.Service)
			return ok && svc.Spec.Type == corev1.ServiceTypeLoadBalancer && svc.Spec.LoadBalancerClass != nil &&
				*svc.Spec.LoadBalancerClass == "out-tree-lbc"
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}, builder.WithPredicates(eventFilter)).
		Watches(
			&discoveryv1.EndpointSlice{},
			handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &corev1.Service{}),
		).
		Named("loadbalancer-service").
		Complete(r)
}
