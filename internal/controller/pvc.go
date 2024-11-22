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

	uyuniv1alpha1 "github.com/cbosdo/uyuni-server-operator/api/v1alpha1"
	adm_kubernetes "github.com/uyuni-project/uyuni-tools/mgradm/shared/kubernetes"
	"github.com/uyuni-project/uyuni-tools/shared/kubernetes"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// checkVolumeClaims ensures the PVCs are ready.
func (r *ServerReconciler) checkVolumeClaims(ctx context.Context, server *uyuniv1alpha1.Server) error {
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
					Labels:    kubernetes.GetLabels(partOf, componentServer),
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
				return fmt.Errorf("Failed to create pvc %s (%s)", mount.Name, err)
			}

		} else if err != nil {
			return err
		}
		// Nothing to do: the pvc is already available
	}

	return nil
}

// checkMirror ensures that the mirror PVC is present.
// If the PVC has been found, the result will be nil.
func (r *ServerReconciler) checkMirror(ctx context.Context, server *uyuniv1alpha1.Server) (*ctrl.Result, error) {
	if server.Spec.Volumes.Mirror != "" {
		// Do we already have the PVC?
		foundPvc := &corev1.PersistentVolumeClaim{}
		err := r.Get(ctx, types.NamespacedName{Name: server.Spec.Volumes.Mirror, Namespace: server.Namespace}, foundPvc)
		if err != nil && apierrors.IsNotFound(err) {
			// The PVC is missing, requeuing
			if err := r.setStatusConditions(ctx, server, metav1.Condition{
				Type:   typeAvailableServer,
				Reason: reasonMirrorPvcMissing,
				Status: metav1.ConditionFalse,
				Message: fmt.Sprintf("PersistentVolumeClaim %s not found in namespace %s",
					server.Spec.Volumes.Mirror, server.Namespace,
				),
			}); err != nil {
				return &ctrl.Result{}, err
			}
			return &ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			return &ctrl.Result{}, err
		}
	}
	return nil, nil
}
