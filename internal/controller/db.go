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

// checkDBSecret checks the presence and validity of a basic-auth secret.
func (r *ServerReconciler) checkBasicAuthSecret(
	ctx context.Context,
	server *uyuniv1alpha1.Server,
	namespacedName types.NamespacedName,
) (*ctrl.Result, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, namespacedName, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return &ctrl.Result{}, fmt.Errorf("Failed to get %s secret (%s)", namespacedName.Name, err)
		}
		err := r.setStatusConditions(ctx, server, metav1.Condition{
			Type:   typeAvailableServer,
			Reason: reasonMissingSecret,
			Status: metav1.ConditionFalse,
			Message: fmt.Sprintf("Waiting for %s basic-auth secret in %s namespace",
				namespacedName.Name,
				namespacedName.Namespace,
			),
		})
		if err != nil {
			return &ctrl.Result{}, err
		}
		return &ctrl.Result{Requeue: true}, nil
	}

	// We want basic-auth since postgresql operator requires it too.
	if secret.Type != "kubernetes.io/basic-auth" {
		err := r.setStatusConditions(ctx, server, metav1.Condition{
			Type:   typeAvailableServer,
			Reason: reasonMissingSecret,
			Status: metav1.ConditionTrue,
			Message: fmt.Sprintf("%s secret in %s namespace should be of basic-auth type",
				namespacedName.Name,
				namespacedName.Namespace,
			),
		})
		if err != nil {
			return &ctrl.Result{}, err
		}
		return &ctrl.Result{Requeue: true}, nil
	}
	return nil, nil
}
