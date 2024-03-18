/*
Copyright 2024.

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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Storage struct {
	StorageClass string            `json:"storageClass"`
	Size         resource.Quantity `json:"size"`
}

// EtcdClusterSpec defines the desired state of EtcdCluster
type EtcdClusterSpec struct {
	// Replicas is the count of etcd instances in cluster.
	// +optional
	// +kubebuilder:default:=3
	Replicas uint    `json:"replicas,omitempty"`
	Storage  Storage `json:"storage,omitempty"`
}

const (
	EtcdConditionInitialized = "Initialized"
	EtcdConditionReady       = "Ready"
)

type EtcdCondType string

const (
	EtcdCondTypeInitStarted         EtcdCondType = "InitializationStarted"
	EtcdCondTypeInitComplete        EtcdCondType = "InitializationComplete"
	EtcdCondTypeStatefulSetReady    EtcdCondType = "StatefulSetReady"
	EtcdCondTypeStatefulSetNotReady EtcdCondType = "StatefulSetNotReady"
)

// EtcdClusterStatus defines the observed state of EtcdCluster
type EtcdClusterStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// EtcdCluster is the Schema for the etcdclusters API
type EtcdCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EtcdClusterSpec   `json:"spec,omitempty"`
	Status EtcdClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// EtcdClusterList contains a list of EtcdCluster
type EtcdClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EtcdCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EtcdCluster{}, &EtcdClusterList{})
}
