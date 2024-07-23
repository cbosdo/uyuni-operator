/*
Copyright 2024 The Uyuni Project.

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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	uyuniv1alpha1 "github.com/cbosdo/uyuni-server-operator/api/v1alpha1"
)

const serverFinalizer = "uyuni.uyuni-project.org/serverfinalizer"

const (
	// typeAvailableMemcached represents the status of the Deployment reconciliation
	typeAvailableServer = "Available"
	// typeDegradedMemcached represents the status used when the custom resource is deleted and the finalizer operations are must to occur.
	typeDegradedServer = "Degraded"
)

// ServerReconciler reconciles a Server object
type ServerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=uyuni.uyuni-project.org,resources=servers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=uyuni.uyuni-project.org,resources=servers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=uyuni.uyuni-project.org,resources=servers/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Server object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *ServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	server := &uyuniv1alpha1.Server{}
	err := r.Get(ctx, req.NamespacedName, server)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then, it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			logger.Info("server resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get server")
		return ctrl.Result{}, err
	}

	// Let's just set the status as Unknown when no status are available
	if server.Status.Conditions == nil || len(server.Status.Conditions) == 0 {
		meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
			Type:    typeAvailableServer,
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Starting reconciliation",
		})
		if err = r.Status().Update(ctx, server); err != nil {
			logger.Error(err, "Failed to update Server status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the server Custom Resource after update the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raise the issue "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, server); err != nil {
			logger.Error(err, "Failed to re-fetch server")
			return ctrl.Result{}, err
		}
	}

	// Let's add a finalizer. Then, we can define some operations which should
	// occurs before the custom resource to be deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(server, serverFinalizer) {
		logger.Info("Adding Finalizer for Server")
		if ok := controllerutil.AddFinalizer(server, serverFinalizer); !ok {
			logger.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, server); err != nil {
			logger.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the Memcached instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isMemcachedMarkedToBeDeleted := server.GetDeletionTimestamp() != nil
	if isMemcachedMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(server, serverFinalizer) {
			logger.Info("Performing Finalizer Operations for Server before delete CR")

			// Let's add here an status "Downgrade" to define that this resource begin its process to be terminated.
			meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{Type: typeDegradedServer,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", server.Name)})

			if err := r.Status().Update(ctx, server); err != nil {
				logger.Error(err, "Failed to update Memcached status")
				return ctrl.Result{}, err
			}

			// Perform all operations required before remove the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			r.doFinalizerOperationsForServer(server)

			// TODO(user): If you add operations to the doFinalizerOperationsForServer method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.

			// Re-fetch the server Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, server); err != nil {
				logger.Error(err, "Failed to re-fetch server")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{Type: typeDegradedServer,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", server.Name)})

			if err := r.Status().Update(ctx, server); err != nil {
				logger.Error(err, "Failed to update Server status")
				return ctrl.Result{}, err
			}

			logger.Info("Removing Finalizer for Server after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(server, serverFinalizer); !ok {
				logger.Error(err, "Failed to remove finalizer for Server")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, server); err != nil {
				logger.Error(err, "Failed to remove finalizer for Server")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// TODO(user): your logic here

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: server.Name, Namespace: server.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Trigger a new installation
		err := r.installServer(server)
		if err != nil {
			logger.Error(err, "Failed to define new Deployment resource for Server")

			// The following implementation will update the status
			meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{Type: typeAvailableServer,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Deployment for the custom resource (%s): (%s)", server.Name, err)})

			if err := r.Status().Update(ctx, server); err != nil {
				logger.Error(err, "Failed to update Server status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}
	} else if err != nil {
		logger.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	} else {
		// TODO(user): Perform an upgrade
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{Type: typeAvailableServer,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) created successfully", server.Name)})

	if err := r.Status().Update(ctx, server); err != nil {
		logger.Error(err, "Failed to update Memcached status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// doFinalizerOperationsForServer will perform the required operations before delete the CR.
func (r *ServerReconciler) doFinalizerOperationsForServer(cr *uyuniv1alpha1.Server) {
	// TODO(user): Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	// Note: It is not recommended to use finalizers with the purpose of delete resources which are
	// created and managed in the reconciliation. These ones, such as the Deployment created on this reconcile,
	// are defined as depended of the custom resource. See that we use the method ctrl.SetControllerReference.
	// to set the ownerRef which means that the Deployment will be deleted by the Kubernetes API.
	// More info: https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/

	// The following implementation will raise an event
	r.Recorder.Event(cr, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
			cr.Name,
			cr.Namespace))
}

// installServer runs the uyuni server helm chart.
func (r *ServerReconciler) installServer(server *uyuniv1alpha1.Server) error {
	// TODO(user): Implement me !
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uyuniv1alpha1.Server{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
