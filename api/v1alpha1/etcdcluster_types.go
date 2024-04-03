/*
Copyright 2024 The etcd-operator Authors.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const defaultEtcdImage = "quay.io/coreos/etcd:v3.5.12"

// EtcdClusterSpec defines the desired state of EtcdCluster
type EtcdClusterSpec struct {
	// Replicas is the count of etcd instances in cluster.
	// +optional
	// +kubebuilder:default:=3
	// +kubebuilder:validation:Minimum:=0
	Replicas *int32 `json:"replicas,omitempty"`
	// PodSpec defines the desired state of PodSpec for etcd members. If not specified, default values will be used.
	PodSpec PodSpec `json:"podSpec,omitempty"`
	// PodDisruptionBudget describes PDB resource to create for etcd cluster members. Nil to disable.
	//+optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudget `json:"podDisruptionBudget,omitempty"`
	Storage             StorageSpec                  `json:"storage"`
}

const (
	EtcdConditionInitialized = "Initialized"
	EtcdConditionReady       = "Ready"
)

type EtcdCondType string
type EtcdCondMessage string

const (
	EtcdCondTypeInitStarted           EtcdCondType = "InitializationStarted"
	EtcdCondTypeInitComplete          EtcdCondType = "InitializationComplete"
	EtcdCondTypeWaitingForFirstQuorum EtcdCondType = "WaitingForFirstQuorum"
	EtcdCondTypeStatefulSetReady      EtcdCondType = "StatefulSetReady"
	EtcdCondTypeStatefulSetNotReady   EtcdCondType = "StatefulSetNotReady"
)

const (
	EtcdInitCondNegMessage           EtcdCondMessage = "Cluster initialization started"
	EtcdInitCondPosMessage           EtcdCondMessage = "Cluster managed resources created"
	EtcdReadyCondNegMessage          EtcdCondMessage = "Cluster StatefulSet is not Ready"
	EtcdReadyCondPosMessage          EtcdCondMessage = "Cluster StatefulSet is Ready"
	EtcdReadyCondNegWaitingForQuorum EtcdCondMessage = "Waiting for first quorum to be established"
)

// EtcdClusterStatus defines the observed state of EtcdCluster
type EtcdClusterStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// EtcdCluster is the Schema for the etcdclusters API
type EtcdCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EtcdClusterSpec   `json:"spec,omitempty"`
	Status EtcdClusterStatus `json:"status,omitempty"`
}

// CalculateQuorumSize returns minimum quorum size for current number of replicas
func (r *EtcdCluster) CalculateQuorumSize() int {
	return int(*r.Spec.Replicas)/2 + 1
}

// +kubebuilder:object:root=true

// EtcdClusterList contains a list of EtcdCluster
type EtcdClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EtcdCluster `json:"items"`
}

// EmbeddedObjectMetadata contains a subset of the fields included in k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta
// Only fields which are relevant to embedded resources are included.
type EmbeddedObjectMetadata struct {
	// Name must be unique within a namespace. Is required when creating resources, although
	// some resources may allow a client to request the generation of an appropriate name
	// automatically. Name is primarily intended for creation idempotence and configuration
	// definition.
	// Cannot be updated.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names#names
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// Labels Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	// +optional
	Labels map[string]string `json:"labels,omitempty" protobuf:"bytes,11,rep,name=labels"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`
}

// PodSpec defines the desired state of PodSpec for etcd members.
// +k8s:openapi-gen=true
type PodSpec struct {
	// Image is the etcd container image name
	// +optional
	Image string `json:"image,omitempty"`
	// ImagePullPolicy describes a policy for if/when to pull a container image
	// +kubebuilder:default:=IfNotPresent
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see https://kubernetes.io/docs/concepts/containers/images/#referring-to-an-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// PodMetadata contains metadata relevant to a PodSpec.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"metadata,omitempty"`
	// Resources describes the compute resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// Affinity sets the scheduling constraints for the pod.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// TopologySpreadConstraints describes how a group of pods ought to spread across topology domains.
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// Tolerations is a list of tolerations.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`
	// PriorityClassName is the name of the PriorityClass for this pod.
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// TerminationGracePeriodSeconds is the time to wait before forceful pod shutdown.
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// SchedulerName is the name of the scheduler to be used for scheduling the pod.
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`
	// RuntimeClassName refers to a RuntimeClass object in the node.k8s.io group, which should be used to run this pod.
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`
	// ExtraArgs are the extra arguments to pass to the etcd container.
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// ExtraEnv are the extra environment variables to pass to the etcd container.
	// +optional
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`

	// LivenessProbe defines liveness probe check for the pod.
	// If not specified, default probe will be used with HTTP probe handler and path /livez on the port 2379,
	// with periodSeconds 5.
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// ReadinessProbe defines readiness probe check for the pod.
	// If not specified, default probe will be used with HTTP probe handler and path /readyz on the port 2379,
	// with periodSeconds 5.
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// StartupProbe defines startup probe check for the pod.
	// If not specified, default probe will be used with HTTP probe handler and path /readyz?serializable=false on the port 2379,
	// with periodSeconds 5.
	// +optional
	StartupProbe *corev1.Probe `json:"startupProbe,omitempty"`
}

// StorageSpec defines the configured storage for a etcd members.
// If neither `emptyDir` nor `volumeClaimTemplate` is specified, then by default an [EmptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) will be used.
// +k8s:openapi-gen=true
type StorageSpec struct {
	// EmptyDirVolumeSource to be used by the StatefulSets. If specified, used in place of any volumeClaimTemplate. More
	// info: https://kubernetes.io/docs/concepts/storage/volumes/#emptydir
	// +optional
	EmptyDir *corev1.EmptyDirVolumeSource `json:"emptyDir,omitempty"`
	// A PVC spec to be used by the StatefulSets.
	// +optional
	VolumeClaimTemplate EmbeddedPersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
}

// EmbeddedPersistentVolumeClaim is an embedded version of k8s.io/api/core/v1.PersistentVolumeClaim.
// It contains TypeMeta and a reduced ObjectMeta.
type EmbeddedPersistentVolumeClaim struct {
	metav1.TypeMeta `json:",inline"`

	// EmbeddedMetadata contains metadata relevant to an EmbeddedResource.
	// +optional
	EmbeddedObjectMetadata `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec defines the desired characteristics of a volume requested by a pod author.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Spec corev1.PersistentVolumeClaimSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current information/status of a persistent volume claim.
	// Read-only.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Status corev1.PersistentVolumeClaimStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// EmbeddedPodDisruptionBudget describes PDB resource for etcd cluster members
type EmbeddedPodDisruptionBudget struct {
	// EmbeddedMetadata contains metadata relevant to an EmbeddedResource.
	// +optional
	EmbeddedObjectMetadata `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	// Spec defines the desired characteristics of a PDB.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/disruptions/#pod-disruption-budgets
	//+optional
	Spec PodDisruptionBudgetSpec `json:"spec"`
}

type PodDisruptionBudgetSpec struct {
	// MinAvailable describes minimum ready replicas. If both are empty, controller will implicitly
	// calculate MaxUnavailable based on number of replicas
	// Mutually exclusive with MaxUnavailable.
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`
	// MinAvailable describes maximum not ready replicas. If both are empty, controller will implicitly
	// calculate MaxUnavailable based on number of replicas
	// Mutually exclusive with MinAvailable
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

func init() {
	SchemeBuilder.Register(&EtcdCluster{}, &EtcdClusterList{})
}
