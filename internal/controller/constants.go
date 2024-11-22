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

// partOf is the value of the partOf label for all resources.
const partOf = "uyuni"

// componentServer is the value of the component label for the big server deployment and pod.
const componentServer = "server"

const (
	// typeAvailableServer represents the status of the Deployment reconciliation
	typeAvailableServer = "Available"
	// typePendingTask represents the status of the server running a job as a stage of its install/update.
	// The Reason of this condition is the name of the pending job.
	typePendingTask = "PendingTask"
	// typeInitialized indicates that the server setup job has been performed.
	typeInitialized = "Initialized"
)

// Reasons for the Progressing condition.
const (
	// reasonReconciled is the reason for the Available condition indicating that the server changes have been performed.
	reasonReconciled = "Reconciled"
	// reasonFailure is the reason for the Available indicating that the server couldn't be updated.
	// The message will provide more information on the actual problem.
	reasonFailure = "Failure"
	// reasonMirrorPvcMissing is the reason value when the server requires a mirror PVC but can't find it.
	reasonMirrorPvcMissing = "MirrorPvcMissing"
	// reasonMissingSecret is the reason value when the server is waiting for a secret to be ready.
	reasonMissingSecret = "MissingSecret"

	// reasonSetup is the reason for typePendingTask condition indicating the started job is the setup.
	reasonSetup = "Setup"
	// reasonCompleted is a reason for the job-related conditions indicating that the stage has successfully finished.
	reasonCompleted = "Completed"
)

const (
	// jobKindSetup is the job kind label value for the setup job.
	jobKindSetup = "setup"
)

// jobKindLabel is the name of the job label defining its stage.
const jobKindLabel = "job-kind"

// jobKindConditions maps the job kind label values to a condition type.
//
// For instance, this means that a setup job kind will have its status indicated in an Initialized condition.
var jobKindConditions = map[string]string{
	jobKindSetup: typeInitialized,
}
