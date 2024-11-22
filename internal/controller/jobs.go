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
	"strings"

	uyuniv1alpha1 "github.com/cbosdo/uyuni-server-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// checkPendingTask verifies if there is a pending task condition and update its status or wait.
func (r *ServerReconciler) checkPendingTask(ctx context.Context, server *uyuniv1alpha1.Server) (*ctrl.Result, error) {
	if condition := meta.FindStatusCondition(server.Status.Conditions, typePendingTask); condition != nil {
		parts := strings.SplitN(condition.Message, ":", 2)
		if len(parts) != 2 {
			log.FromContext(ctx).Error(errors.New("Invalid PendingTask condition message"),
				"PendingTask message has to end with a semicolon followed by the job name",
				"message", condition.Message,
			)
			// If that happens it's a developers's problem, not the user.
			return &ctrl.Result{}, nil
		}
		jobName := strings.TrimSpace(parts[1])
		job := &batchv1.Job{}
		err := r.Get(ctx, types.NamespacedName{Namespace: server.Namespace, Name: jobName}, job)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to get the pending job", "jobName", jobName)
			return &ctrl.Result{}, err
		}
		// These checks assume that all our jobs are running a single pod.
		if job.Status.Succeeded > 0 {
			log.FromContext(ctx).Info("Job succeeded", "jobname", jobName)
			// Mark the task finished
			if err := r.setStatusConditions(ctx, server, metav1.Condition{
				Type:    jobKindConditions[job.ObjectMeta.Labels[jobKindLabel]],
				Reason:  reasonCompleted,
				Status:  metav1.ConditionTrue,
				Message: fmt.Sprintf("The %s job succeeded", jobName),
			}); err != nil {
				return &ctrl.Result{}, err
			}
			if err := r.removeStatusCondition(ctx, server, typePendingTask); err != nil {
				return &ctrl.Result{}, err
			}
		} else if job.Status.Failed > 0 {
			log.FromContext(ctx).Info("Job failed", "jobname", jobName)
			// Mark the server as failed
			if err = r.setStatusConditions(ctx, server,
				metav1.Condition{
					Type:    jobKindConditions[job.ObjectMeta.Labels[jobKindLabel]],
					Reason:  reasonFailure,
					Status:  metav1.ConditionFalse,
					Message: fmt.Sprintf("The %s job failed", jobName),
				},
				metav1.Condition{
					Type:    typeAvailableServer,
					Reason:  reasonFailure,
					Status:  metav1.ConditionFalse,
					Message: fmt.Sprintf("The %s job failed", jobName),
				},
			); err != nil {
				return &ctrl.Result{}, err
			}
			return &ctrl.Result{}, nil
		}
		// Wait a bit more
		return &ctrl.Result{Requeue: true}, nil
	}
	return nil, nil
}
