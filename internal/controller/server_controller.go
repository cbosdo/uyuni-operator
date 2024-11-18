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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
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

const serverFinalizer = "uyuni.uyuni-project.org/serverfinalizer"

const (
	// typeAvailableServer represents the status of the Deployment reconciliation
	typeAvailableServer = "Available"
	// typeDegradedServer represents the status used when the server is deleted and the finalizer operations are to occur.
	typeDegradedServer = "Degraded"
	// typeProgressingServer represents the status of the server being setup or upgraded.
	typeProgressingServer = "Progressing"
)

const (
	// reasonMirrorPvcMissing is the reason value when the server requires a mirror PVC but can't find it.
	reasonMirrorPvcMissing = "MirrorPvcMissing"
	// reasonMissingTLSSecret is the reason value when the server is waiting for the TLS secret to be ready.
	reasonMissingTLSSecret = "MissingTLSSecret"
	// reasonMissingDBSSecret is the reason value when the server is waiting for the database secret to be ready.
	reasonMissingDBSecret = "MissingDBSecret"
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

	// Let's just set the status as Unknown when no status are available
	if len(server.Status.Conditions) == 0 {
		logger.Info("Starting reconciliation for server")
		err := r.setStatusCondition(ctx, req, server, metav1.Condition{
			Type:    typeAvailableServer,
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Starting reconciliation",
		})
		if err != nil {
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

	// TODO Start a data sync job for migration if requested

	// TODO Do we have a running data sync job to wait for?

	// TODO Do we already have a DB volume to inspect for upgrade?

	// Create PersistentVolumeClaims if needed
	if err := r.checkVolumeClaims(ctx, server); err != nil {
		return ctrl.Result{}, err
	}

	// TODO Upgrade existing DB if needed

	// TODO Run the DB finalization job if needed

	// TODO Run the post upgrade job if needed

	// Are the secrets available?
	if !r.hasTLSSecret(ctx, types.NamespacedName{
		Namespace: server.Namespace, Name: server.Spec.SSL.ServerSecretName,
	}) {
		err := r.setStatusCondition(ctx, req, server, metav1.Condition{
			Type:   typeProgressingServer,
			Reason: reasonMissingTLSSecret,
			Status: metav1.ConditionTrue,
			Message: fmt.Sprintf("Waiting for %s TLS secret in %s namespace",
				server.Spec.SSL.ServerSecretName,
				server.Namespace,
			),
		})
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Create the ingresses
	if err := r.checkIngresses(ctx, server); err != nil {
		return ctrl.Result{}, err
	}

	// Check for the mirror PV if needed
	if res, err := r.checkMirror(ctx, server); res != nil {
		return *res, err
	}

	// Check that we have the DB credentials secret
	if valid, err := r.checkDBSecret(
		ctx, req, server,
		types.NamespacedName{Namespace: server.Namespace, Name: server.Spec.DB.CredentialsSecret},
	); !valid {
		return ctrl.Result{}, err
	}

	if valid, err := r.checkDBSecret(
		ctx, req, server,
		types.NamespacedName{Namespace: server.Namespace, Name: server.Spec.ReportDB.CredentialsSecret},
	); !valid {
		return ctrl.Result{}, err
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
			if err := r.setStatusCondition(ctx, req, server, metav1.Condition{
				Type:   typeAvailableServer,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Deployment (%s)", err),
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
	if err := r.checkServices(ctx, server); err != nil {
		return ctrl.Result{}, err
	}

	// TODO Create the coco deployment if needed

	// TODO Create the Hub API deployment if needed

	// TODO Create the hub service if needed

	// The following implementation will update the status
	if err := r.setStatusCondition(ctx, req, server, metav1.Condition{
		Type:   typeAvailableServer,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: "Server resources created successfully",
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ServerReconciler) setStatusCondition(
	ctx context.Context,
	req ctrl.Request,
	server *uyuniv1alpha1.Server,
	condition metav1.Condition,
) error {
	logger := log.FromContext(ctx)

	// Ensure the server instance is the latest
	if err := r.Get(ctx, req.NamespacedName, server); err != nil {
		logger.Error(err, "Failed to re-fetch server")
		return err
	}

	meta.SetStatusCondition(&server.Status.Conditions, condition)

	if err := r.Status().Update(ctx, server); err != nil {
		logger.Error(err, "Failed to update Server status")
		return err
	}

	if err := r.Get(ctx, req.NamespacedName, server); err != nil {
		logger.Error(err, "Failed to re-fetch server after setting condition")
		return err
	}
	return nil
}

// checkVolumeClaims ensures the PVCs are ready.
func (r *ServerReconciler) checkVolumeClaims(ctx context.Context, server *uyuniv1alpha1.Server) error {
	logger := log.FromContext(ctx)

	// Compute the list of PVCs we need
	mounts := adm_kubernetes.GetServerMounts()
	mounts = tuneMounts(mounts, &server.Spec.Volumes)

	for _, mount := range mounts {
		pvc := corev1.PersistentVolumeClaim{}
		err := r.Get(ctx, types.NamespacedName{Namespace: server.Namespace, Name: mount.Name}, &pvc)
		if err != nil && apierrors.IsNotFound(err) {
			size := mount.Size
			if size == "" {
				size = "10Mi"
			}

			// Create the PVC
			pvc = corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: server.Namespace,
					Name:      mount.Name,
					Labels:    labelsForServer(),
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{"storage": resource.MustParse(size)},
					},
				},
			}

			if err := ctrl.SetControllerReference(server, &pvc, r.Scheme); err != nil {
				return err
			}

			if err := r.Create(ctx, &pvc); err != nil {
				logger.Error(err, "Failed to create pvc %s", mount.Name)
				return err
			}

		} else if err != nil {
			return err
		}
		// Nothing to do: the pvc is already available
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

// checkMirror ensures that the mirror PVC is present.
// If the PVC has been found, the result will be nil.
func (r *ServerReconciler) checkMirror(ctx context.Context, server *uyuniv1alpha1.Server) (*ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if server.Spec.Volumes.Mirror != "" {
		// Do we already have the PVC?
		foundPvc := &corev1.PersistentVolumeClaim{}
		err := r.Get(ctx, types.NamespacedName{Name: server.Spec.Volumes.Mirror, Namespace: server.Namespace}, foundPvc)
		if err != nil && apierrors.IsNotFound(err) {
			// The PVC is missing, requeuing
			meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
				Type:   typeProgressingServer,
				Reason: reasonMirrorPvcMissing,
				Status: metav1.ConditionTrue,
				Message: fmt.Sprintf("PersistentVolumeClaim %s not found in namespace %s",
					server.Spec.Volumes.Mirror, server.Namespace,
				),
			})
			if err = r.Status().Update(ctx, server); err != nil {
				logger.Error(err, "Failed to update Server status")
				return &ctrl.Result{}, err
			}
			return &ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			return &ctrl.Result{}, err
		}
	}
	return nil, nil
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

func labelsForServer() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "uyuni-server",
		"app.kubernetes.io/part-of":    "uyuni",
		"app.kubernetes.io/component":  "server",
		"app.kubernetes.io/managed-by": "ServerController",
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uyuniv1alpha1.Server{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
