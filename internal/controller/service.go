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
	"strings"

	uyuniv1alpha1 "github.com/cbosdo/uyuni-server-operator/api/v1alpha1"
	adm_kubernetes "github.com/uyuni-project/uyuni-tools/mgradm/shared/kubernetes"
	"github.com/uyuni-project/uyuni-tools/shared/kubernetes"
	"github.com/uyuni-project/uyuni-tools/shared/utils"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// publicServices is the list of services that support customization.
var publicServices = []string{
	utils.ReportdbServiceName,
	utils.TftpServiceName,
	utils.SaltServiceName,
	utils.CobblerServiceName,
	utils.TaskoServiceName,
	utils.TomcatServiceName,
	utils.SearchServiceName,
}

func (r *ServerReconciler) checkServices(
	ctx context.Context,
	server *uyuniv1alpha1.Server,
) (*ctrl.Result, error) {
	services := adm_kubernetes.GetServices(server.Namespace, server.Spec.Debug)

	managedServices := &corev1.ServiceList{}
	if err := r.List(
		ctx, managedServices,
		client.InNamespace(server.Namespace),
		client.MatchingLabels{"app.kubernetes.io/component": componentServer},
	); err != nil {
		return &ctrl.Result{}, fmt.Errorf("Failed to list the managed services (%s)", err)
	}

	allServices := &corev1.ServiceList{}
	if err := r.List(
		ctx, allServices,
		client.InNamespace(server.Namespace),
	); err != nil {
		return &ctrl.Result{}, fmt.Errorf("Failed to list all the services (%s)", err)
	}

	customServices := []string{}
	for _, svc := range allServices.Items {
		if !slices.ContainsFunc(managedServices.Items, func(cmp corev1.Service) bool {
			return svc.ObjectMeta.Name == cmp.ObjectMeta.Name
		}) {
			customServices = append(customServices, svc.ObjectMeta.Name)
		}
	}

	// Apply each service
	for _, svc := range services {
		name := svc.ObjectMeta.Name
		overridable := strings.HasSuffix(name, "db")
		if overridable && !slices.Contains(customServices, svc.ObjectMeta.Name) || !overridable {
			svc.ObjectMeta.Labels = kubernetes.GetLabels(partOf, "")

			// Apply the service customization. This should be shared with mgradm.
			if slices.Contains(publicServices, svc.ObjectMeta.Name) {
				svc.Spec.Type = corev1.ServiceType(server.Spec.Services.Type)
				for k, v := range server.Spec.Services.Annotations {
					svc.ObjectMeta.Annotations[k] = v
				}

				for k, v := range server.Spec.Services.Labels {
					svc.ObjectMeta.Labels[k] = v
				}
			}

			// Apply changes if needed
			if slices.ContainsFunc(allServices.Items, func(cmp corev1.Service) bool {
				return svc.ObjectMeta.Name == cmp.ObjectMeta.Name
			}) {
				// TODO Look for Update?
			} else {
				// Create the missing service
				if err := ctrl.SetControllerReference(server, svc, r.Scheme); err != nil {
					return &ctrl.Result{}, err
				}
				if err := r.Create(ctx, svc); err != nil {
					return &ctrl.Result{}, fmt.Errorf("Failed to create new %s service (%s)", name, err)
				}
			}
		}
	}

	// TODO Remove remaining services that are linked to the server resource

	return nil, nil
}
