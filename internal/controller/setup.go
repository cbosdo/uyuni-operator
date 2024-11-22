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
	"errors"
	"fmt"
	"time"

	uyuniv1alpha1 "github.com/cbosdo/uyuni-server-operator/api/v1alpha1"
	"github.com/uyuni-project/uyuni-tools/mgradm/shared/kubernetes"
	"github.com/uyuni-project/uyuni-tools/mgradm/shared/utils"
	"github.com/uyuni-project/uyuni-tools/shared/api/types"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// checkSetup ensures that the setup job has been started.
func (r *ServerReconciler) checkSetup(ctx context.Context, server *uyuniv1alpha1.Server) (*ctrl.Result, error) {
	condition := meta.FindStatusCondition(server.Status.Conditions, typeInitialized)
	if condition == nil {
		jobName, err := r.createSetupJob(ctx, server)
		if err != nil {
			if err := r.setStatusConditions(ctx, server, metav1.Condition{
				Type:    typeAvailableServer,
				Status:  metav1.ConditionTrue,
				Reason:  reasonFailure,
				Message: fmt.Sprintf("Failed to create the setup job (%s)", err),
			}); err != nil {
				return &ctrl.Result{}, err
			}
			// Maybe we'll be more lucky next time
			return &ctrl.Result{Requeue: true}, nil
		}
		if err := r.setStatusConditions(ctx, server, metav1.Condition{
			Type:   typePendingTask,
			Status: metav1.ConditionTrue,
			Reason: reasonSetup,
			// This message format is important as it's where we find the job name later.
			Message: fmt.Sprintf("Running setup in job (%s)", jobName),
		}); err != nil {
			return &ctrl.Result{}, err
		}
		// No need to requeue too fast: setup takes time
		return &ctrl.Result{Requeue: true, RequeueAfter: 15 * time.Second}, nil
	}
	return nil, nil
}

func (r *ServerReconciler) createSetupJob(
	ctx context.Context,
	server *uyuniv1alpha1.Server,
) (string, error) {
	logger := log.FromContext(ctx)

	if server.Spec.AdminCredentialsSecret == "" {
		return "", errors.New("Missing spec.adminCredentialsSecret")
	}
	if server.Spec.DB.CredentialsSecret == "" {
		return "", errors.New("Missing spec.db.adminCredentialsSecret")
	}
	if server.Spec.ReportDB.CredentialsSecret == "" {
		return "", errors.New("Missing spec.reportdb.adminCredentialsSecret")
	}

	flags := utils.InstallationFlags{
		TZ:           server.Spec.Timezone,
		Debug:        utils.DebugFlags{Java: server.Spec.Debug},
		Email:        server.Spec.Email,
		EmailFrom:    server.Spec.Email,
		Organization: "Organization",
		Admin: types.User{
			FirstName: "Administrator",
			LastName:  "McAdmin",
			Email:     server.Spec.Email,
		},
		DB: utils.DBFlags{
			Host: "localhost", // TODO This will need to be changed when DB is extracted
			Name: server.Spec.DB.Name,
		},
		ReportDB: utils.DBFlags{
			Host: server.Spec.ReportDB.Host, // TODO Change when db is extracted
			Name: server.Spec.ReportDB.Name,
		},
		IssParent: "", // No support for ISSv1 in kubernetes!
	}

	job, err := kubernetes.GetSetupJob(
		server.Namespace,
		server.Spec.Image,
		v1.PullPolicy(server.Spec.PullPolicy),
		server.Spec.PullSecret,
		server.Spec.Volumes.Mirror,
		&flags,
		server.Spec.Fqdn,
		server.Spec.AdminCredentialsSecret,
		server.Spec.DB.CredentialsSecret,
		server.Spec.ReportDB.CredentialsSecret,
		"", // Let the user set the SCC credentials once the server is up
	)
	if err != nil {
		return "", err
	}

	labels := labelsForServer()
	labels[jobKindLabel] = jobKindSetup
	job.ObjectMeta.Labels = labels

	if err := ctrl.SetControllerReference(server, job, r.Scheme); err != nil {
		return "", err
	}

	logger.Info("Creating a new setup job", "job.name", job.ObjectMeta.Name)
	if err = r.Create(ctx, job); err != nil {
		return "", err
	}

	return job.ObjectMeta.Name, nil
}
