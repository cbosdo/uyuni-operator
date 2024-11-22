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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	uyuniv1alpha1 "github.com/cbosdo/uyuni-server-operator/api/v1alpha1"
	adm_kubernetes "github.com/uyuni-project/uyuni-tools/mgradm/shared/kubernetes"
	uyuni_types "github.com/uyuni-project/uyuni-tools/shared/types"
)

// ServerReconciler reconciles a Server object
type ServerReconciler struct {
	client.Client
	RESTClient rest.Interface
	RESTConfig *rest.Config
	RESTMapper *meta.RESTMapper
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
}

//+kubebuilder:rbac:groups=uyuni.uyuni-project.org,resources=servers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=uyuni.uyuni-project.org,resources=servers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=uyuni.uyuni-project.org,resources=servers/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
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

	// Update the PendingTask condition and wait if it's still pending
	if res, err := r.checkPendingTask(ctx, server); res != nil {
		return *res, err
	}

	// TODO Start a data sync job for migration if requested
	// TODO When starting a migration, add the Initialized condition to true to skip setup

	// TODO Do we have a running data sync job to wait for?

	// TODO Do we already have a DB volume to inspect for upgrade?

	// Create PersistentVolumeClaims if needed
	if err := r.checkVolumeClaims(ctx, server); err != nil {
		if err := r.setStatusConditions(ctx, server, metav1.Condition{
			Type:    typeAvailableServer,
			Reason:  reasonFailure,
			Status:  metav1.ConditionFalse,
			Message: err.Error(),
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	// TODO Upgrade existing DB if needed

	// TODO Run the DB finalization job if needed

	// TODO Run the post upgrade job if needed

	if res, err := r.checkTLSSecret(ctx, server); res != nil {
		return *res, err
	}

	if res, err := r.checkIngresses(ctx, server); res != nil {
		return *res, err
	}

	if res, err := r.checkMirror(ctx, server); res != nil {
		return *res, err
	}

	// Check that we have the DB credentials secret
	if res, err := r.checkBasicAuthSecret(
		ctx, server,
		types.NamespacedName{Namespace: server.Namespace, Name: server.Spec.DB.CredentialsSecret},
	); res != nil {
		return *res, err
	}

	if res, err := r.checkBasicAuthSecret(
		ctx, server,
		types.NamespacedName{Namespace: server.Namespace, Name: server.Spec.ReportDB.CredentialsSecret},
	); res != nil {
		return *res, err
	}

	if res, err := r.checkSetup(ctx, server); res != nil {
		return *res, err
	}

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: adm_kubernetes.ServerDeployName, Namespace: server.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Trigger a new installation
		err := r.installServer(ctx, server)
		if err != nil {
			logger.Error(err, "Failed to define new Deployment resource for Server")
			if err := r.Get(ctx, req.NamespacedName, server); err != nil {
				logger.Error(err, "Failed to re-fetch server")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			if err := r.setStatusConditions(ctx, server, metav1.Condition{
				Type:    typeAvailableServer,
				Status:  metav1.ConditionFalse,
				Reason:  reasonFailure,
				Message: fmt.Sprintf("Failed to create the uyuni deployment (%s)", err),
			}); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}
	} else if err != nil {
		logger.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	} else {
		// TODO(user): Perform an upgrade if the status indicates it has been completed.
	}

	// Create the services if needed
	if res, err := r.checkServices(ctx, server); res != nil {
		return *res, err
	}

	// TODO Create the coco deployment if needed

	// TODO Create the Hub API deployment if needed

	// TODO Create the hub service if needed

	// The following implementation will update the status
	if err := r.setStatusConditions(ctx, server, metav1.Condition{
		Type:    typeAvailableServer,
		Status:  metav1.ConditionTrue,
		Reason:  reasonReconciled,
		Message: "Server resources created successfully",
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ServerReconciler) setStatusConditions(
	ctx context.Context,
	server *uyuniv1alpha1.Server,
	conditions ...metav1.Condition,
) error {
	logger := log.FromContext(ctx)
	namespacedName := types.NamespacedName{Namespace: server.Namespace, Name: server.Name}

	// Ensure the server instance is the latest
	if err := r.Get(ctx, namespacedName, server); err != nil {
		logger.Error(err, "Failed to re-fetch server")
		return err
	}

	changed := false
	for _, condition := range conditions {
		changed = changed || meta.SetStatusCondition(&server.Status.Conditions, condition)
	}

	if changed {
		if err := r.Status().Update(ctx, server); err != nil {
			logger.Error(err, "Failed to update Server status")
			return err
		}

		if err := r.Get(ctx, namespacedName, server); err != nil {
			logger.Error(err, "Failed to re-fetch server after setting conditions")
			return err
		}
	}
	return nil
}

func (r *ServerReconciler) removeStatusCondition(
	ctx context.Context,
	server *uyuniv1alpha1.Server,
	conditionType string,
) error {
	logger := log.FromContext(ctx)
	namespacedName := types.NamespacedName{Namespace: server.Namespace, Name: server.Name}

	// Ensure the server instance is the latest
	if err := r.Get(ctx, namespacedName, server); err != nil {
		logger.Error(err, "Failed to re-fetch server")
		return err
	}

	if meta.RemoveStatusCondition(&server.Status.Conditions, conditionType) {
		if err := r.Status().Update(ctx, server); err != nil {
			logger.Error(err, "Failed to update Server status")
			return err
		}

		if err := r.Get(ctx, namespacedName, server); err != nil {
			logger.Error(err, "Failed to re-fetch server after setting condition")
			return err
		}
	}
	return nil
}

func tuneMounts(mounts []uyuni_types.VolumeMount, volumesSpec *uyuniv1alpha1.Volumes) []uyuni_types.VolumeMount {
	tunedMounts := []uyuni_types.VolumeMount{}
	for _, mount := range mounts {
		class := volumesSpec.Class
		var volumeSpec *uyuniv1alpha1.Volume
		switch mount.Name {
		case "var-pgsql":
			volumeSpec = &volumesSpec.Database
		case "var-spacewalk":
			volumeSpec = &volumesSpec.Packages
		case "var-cache":
			volumeSpec = &volumesSpec.Cache
		case "srv-www":
			volumeSpec = &volumesSpec.Www
		}
		if volumeSpec != nil {
			if volumeSpec.Class != "" {
				class = volumeSpec.Class
			}
			mount.Size = volumeSpec.Size
		}
		mount.Class = class
		tunedMounts = append(tunedMounts, mount)
	}
	return tunedMounts
}

// installServer runs the uyuni server helm chart.
func (r *ServerReconciler) installServer(ctx context.Context, server *uyuniv1alpha1.Server) error {
	log := log.FromContext(ctx)

	serverDep, err := r.serverDeployment(server)
	if err != nil {
		log.Error(err, "Failed to define new Deployment resource for Server")

		// The following implementation will update the status
		meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{Type: typeAvailableServer,
			Status: metav1.ConditionFalse, Reason: "Reconciling",
			Message: fmt.Sprintf("Failed to create server deployment: (%s)", err)})

		if err := r.Status().Update(ctx, server); err != nil {
			log.Error(err, "Failed to update Server status")
			return err
		}

		return err
	}

	log.Info("Creating a new Deployment",
		"Deployment.Namespace", serverDep.Namespace, "Deployment.Name", serverDep.Name)
	if err = r.Create(ctx, serverDep); err != nil {
		log.Error(err, "Failed to create new Deployment",
			"Deployment.Namespace", serverDep.Namespace, "Deployment.Name", serverDep.Name)
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uyuniv1alpha1.Server{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
