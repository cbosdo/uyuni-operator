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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServerSpec defines the desired state of Server
type ServerSpec struct {
	// Fqdn defines the Fully Qualified Domain Name to use for the Uyuni server.
	//+kubebuilder:validation:Pattern=`^([a-zA-Z0-9]{1}[a-zA-Z0-9-]{0,62})(\.[a-zA-Z0-9]{1}[a-zA-Z0-9-]{0,62})*?(\.[a-zA-Z]{1}[a-zA-Z0-9]{0,62})\.?$`
	//+required
	Fqdn string `json:"fqdn"`

	// Image defines the image to use for the big server container.
	//+kubebuilder:default=`registry.opensuse.org/uyuni/server:latest`
	Image string `json:"image,omitempty"`

	// PullPolicy defines the pull policy for all images used by the server.
	PullPolicy string `json:"pullPolicy,omitempty"`

	// PullSecret defines the name of the secret to use to pull the images from the registry.
	PullSecret string `json:"pullSecret,omitempty"`

	// Timezone defines the time zone to set in the server containers.
	Timezone string `json:"timezone,omitempty"`

	// Email defines the email for the server administrator
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=128
	Email string `json:"email,omitempty"`

	// EmailFrom defines the email used to send the notification emails.
	EmailFrom string `json:"email_from,omitempty"`

	// Volumes defines configuration for the server persistent volumes claims.
	// Changing the persistent volumes claims storage class or size after the initial creation will have no effect.
	Volumes Volumes `json:"volumes,omitempty"`

	// Debug defines whether the Java debug ports should be enabled and exposed.
	// This is not recommended on a production server.
	//+kubebuilder:default=false
	Debug bool `json:"debug,omitempty"`

	// SSL defines how to get the SSL certificates.
	// It can be using existing TLS secrets or cert-manager issuers.
	// If nothing is provided
	SSL `json:"ssl,omitempty"`

	// Ingress defines the name of the ingress for which to generate rules.
	// Accepted values are nginx or traefik.
	// If anything else if provided, no Ingress rule will be generated.
	Ingress string `json:"ingress,omitempty"`

	// DB defines how to access and configure the internal database.
	//+required
	DB `json:"db"`

	// ReportDB defines how to access and configure the report database.
	//+required
	ReportDB `json:"reportdb"`

	// AdminCredentialsSecret defines the name of the secret containing the first user credentials.
	// This secret is only used during the installation and can be disposed of after.
	// A secret of basic-auth type is required here.
	AdminCredentialsSecret string `json:"adminCredentialsSecret,omitempty"`

	Services `json:"services,omitempty"`
	// TODO Add flags to set:
	// - replicas for Hub XML-RPC API, Coco attestation
	// - enable / disable tftp
	// - images
}

// SSL defines how to access the SSL certificates.
type SSL struct {
	// ServerSecretName defines the name of the Secret containing the server TLS secret.
	// The secret can either be generated by cert-manager or statically defined.
	// The TLS secret is expected to contain the CA certificate in the ca.crt item.
	//+kubebuilder:default=`uyuni-server-tls`
	ServerSecretName string `json:"ServerSecretName,omitempty"`
	// IssuerName defines the name of the cert-manager issuer to use for ingress rules.
	// Leave empty if cert-manager is not used.
	IssuerName string `json:"issuerName,omitempty"`
}

// Volumes defines the server storage configuration
type Volumes struct {
	// Class is the default storage class for all the persistent volume claims.
	Class string `json:"class,omitempty"`
	// Database is the configuration of the var-pgsql volume.
	Database Volume `json:"database,omitempty"`
	// Packages is the configuration of the var-spacewalk volume containing the synchronizede repositories.
	Packages Volume `json:"packages,omitempty"`
	// Www is the configuration of the srv-www volume containing the imags and distributions.
	Www Volume `json:"www,omitempty"`
	// Cache is the configuration of the var-cache volume.
	Cache Volume `json:"cache,omitempty"`
	// Mirror defines the name of a persistent volume claim to use as mirror by the server.
	// The persistent volume claim is assumed to be existing, bound and populated.
	// No mirror will be set up if omitted.
	Mirror string `json:"mirror,omitempty"`
}

// Volume defines the configuration of a single persistent volume claim.
type Volume struct {
	// Size is the requested size of the volume using kubernetes values like '100Gi'.
	Size string `json:"size,omitempty"`
	// Class is the storage class of the volume.
	Class string `json:"class,omitempty"`
}

// DB defines the configuration of a database.
type DB struct {
	// CredentialsSecret defines the name of the secret containing the credentials to use.
	// The credentials secrets are expected to container username and password items.
	//+required
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
	// AdminCredentialsSecret defines the name of the secret containing the admin credentials to use.
	// This is only needed for an external Database.
	// The credentials secrets are expected to container username and password items.
	AdminCredentialsSecret string `json:"adminCredentialsSecret,omitempty"`
	// Host defines the FQDN to use to connect to, for an external database.
	//
	// This can be omitted for an external database if the corresponding service is created outside
	// of the operator. A service named db would describe how to access the internal database.
	Host string `json:"host,omitempty"`
	// Port defines the port to connect to, for an external database.
	//
	// Like Host, this can be omitted if the service is created outside of the operator.
	Port int `json:"port,omitempty"`
	// Name defines the name of the database to connect to.
	//+kubebuilder:default=`uyuni`
	Name string `json:"name,omitempty"`
}

// ReportDB defines the configuration of a report database.
type ReportDB struct {
	// CredentialsSecret defines the name of the secret containing the credentials to use.
	// The credentials secrets are expected to container username and password items.
	//+required
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
	// Host defines the FQDN to use to connect to, for an external database.
	//
	// This can be omitted for an external database if the corresponding service is created outside
	// of the operator. A service named reportdb would describe how to access the report database.
	Host string `json:"host,omitempty"`
	// Port defines the port to connect to, for an external database.
	//
	// Like Host, this can be omitted if the service is created outside of the operator.
	Port int `json:"port,omitempty"`
	// Name defines the name of the database to connect to.
	//+kubebuilder:default=`reportdb`
	Name string `json:"name,omitempty"`
}

// Services defines the configuration of the services.
type Services struct {
	// Type defines the service type of the public services.
	// This will not affect internal services that are not supposed to be accessed by the user.
	//+kubebuilder:default=`ClusterIP`
	Type string `json:"type,omitempty"`
	// Annotations defines annotations to set on all the public services.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Labels defines additional labels to set on all the publis services.
	Labels map[string]string `json:"labels,omitempty"`
}

// ServerStatus defines the observed state of Server
type ServerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Represents the observations of an Uyuni server's current state.
	// Server.status.conditions.type are: "Available", "Degraded"
	// Server.status.conditions.status are one of True, False, Unknown.
	// Server.status.conditions.reason the value should be a CamelCase string and producers of specific
	// condition types may define expected values and meanings for this field, and whether the values
	// are considered a guaranteed API.
	// Server.status.conditions.Message is a human readable message indicating details about the transition.
	// For further information see: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// Conditions store the status conditions of the Server instances
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="FQDN",type=string,JSONPath=`.spec.fqdn`

// Server is the Schema for the servers API
type Server struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServerSpec   `json:"spec,omitempty"`
	Status ServerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ServerList contains a list of Server
type ServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Server `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Server{}, &ServerList{})
}
