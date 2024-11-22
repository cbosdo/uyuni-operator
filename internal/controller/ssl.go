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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// checkTLSSecret ensures the TLS secret is ready and sets condition accordingly.
func (r *ServerReconciler) checkTLSSecret(ctx context.Context, server *uyuniv1alpha1.Server) (*ctrl.Result, error) {
	name := server.Spec.SSL.ServerSecretName
	hasTLSSecret := false
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: server.Namespace, Name: name}, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return &ctrl.Result{}, fmt.Errorf("Failed to get %s secret (%s)", name, err)
		}
	} else {
		hasTLSSecret = secret.Type == "kubernetes.io/tls"
	}

	if !hasTLSSecret {
		err := r.setStatusConditions(ctx, server, metav1.Condition{
			Type:   typeAvailableServer,
			Reason: reasonMissingSecret,
			Status: metav1.ConditionFalse,
			Message: fmt.Sprintf("Waiting for %s TLS secret in %s namespace",
				server.Spec.SSL.ServerSecretName,
				server.Namespace,
			),
		})
		if err != nil {
			return &ctrl.Result{}, err
		}
		return &ctrl.Result{Requeue: true}, nil
	}
	return nil, nil
}