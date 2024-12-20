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
	"slices"

	uyuniv1alpha1 "github.com/cbosdo/uyuni-server-operator/api/v1alpha1"
	adm_kubernetes "github.com/uyuni-project/uyuni-tools/mgradm/shared/kubernetes"
	"github.com/uyuni-project/uyuni-tools/shared/kubernetes"
	net "k8s.io/api/networking/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *ServerReconciler) checkIngresses(
	ctx context.Context,
	server *uyuniv1alpha1.Server,
) (*ctrl.Result, error) {
	logger := log.FromContext(ctx)

	caIssuer := server.Spec.SSL.IssuerName
	ingresses := adm_kubernetes.GetIngresses(
		server.Namespace,
		server.Spec.Fqdn,
		caIssuer,
		server.Spec.Ingress,
	)

	if len(ingresses) == 0 {
		logger.Info("No or unhandled ingress provided, skipping ingress rules creation")
		return nil, nil
	}

	foundIngresses := &net.IngressList{}
	if err := r.List(ctx, foundIngresses); err != nil {
		return &ctrl.Result{}, fmt.Errorf("Failed to list ingresses (%s)", err)
	}

	for _, ingressDef := range ingresses {
		namespace := ingressDef.ObjectMeta.Namespace
		name := ingressDef.ObjectMeta.Name

		ingressDef.ObjectMeta.Name = name
		ingressDef.ObjectMeta.Labels = kubernetes.GetLabels(partOf, "")

		// Check and create
		if slices.ContainsFunc(foundIngresses.Items, func(item net.Ingress) bool {
			return item.ObjectMeta.Name == name && item.ObjectMeta.Namespace == namespace
		}) {
			// TODO Look for update ?
		} else {
			// Create the missing ingress
			if err := ctrl.SetControllerReference(server, ingressDef, r.Scheme); err != nil {
				return &ctrl.Result{}, err
			}
			if err := r.Create(ctx, ingressDef); err != nil {
				return &ctrl.Result{}, fmt.Errorf("Failed to create new %s Ingress (%s)", name, err)
			}
		}
	}

	// TODO Remove remaining ingress that are linked to the server resource

	return nil, nil
}
