/*
Copyright 2022.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	Finalizer = "gezb.io/node-upgrader"
)

// MaintenancePhase contains the phase of maintenance
type MaintenancePhase string

// PhaseReason contains reason the drain has failed
type PhaseReason string

const (
	// MaintenanceInvalid - maintenance request is invaid
	MaintenanceInvalid MaintenancePhase = "Invalid"
	// MaintenanceDryRun - maintenance is not active and
	MaintenanceDryRun MaintenancePhase = "Dry-run"
	// MaintenanceRunning - maintenance has started its proccessing
	MaintenanceRunning MaintenancePhase = "Running"
	// MaintenanceSucceeded - maintenance has finished succesfuly, cordoned the node and evicted all pods (that could be evicted)
	MaintenanceSucceeded MaintenancePhase = "Succeeded"
	// MaintenanceFailed - maintenance has failed
	MaintenanceFailed MaintenancePhase = "Failed"

	FailureReasonNodeNotFound       PhaseReason = "Node specified in the NodeDrain CRD cannot be found"
	FailureReasonInvalidNodeVersion PhaseReason = "Node Version does not match the version in the NodeDrain CRD"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NodeDrainSpec defines the desired state of NodeDrain
type NodeDrainSpec struct {
	Active             bool   `json:"active,omitempty"`
	NodeName           string `json:"nodeName,omitempty"`
	ExpectedK8sVersion string `json:"expectedK8sVersion,omitempty"`
	Drain              bool   `json:"drain,omitempty"`
}

// NodeDrainStatus defines the observed state of NodeDrain
type NodeDrainStatus struct {
	NodeName string `json:"nodeName,omitempty"`
	// Phase is the represtation of the maintenance progress (Running,Succeeded,Failed)
	Phase MaintenancePhase `json:"phase,omitempty"`
	// PhaseReson describes why phase is in an error state
	PhaseReason PhaseReason `json:"phasereason,omitempty"`
	// LastError represents the latest error if any in the latest reconciliation
	LastError string `json:"lastError,omitempty"`
	// PendingPods is a list of pending pods for eviction
	PendingPods []string `json:"pendingPods,omitempty"`
	// RunningPods is a list of running pods for deletion
	RunningPods []string `json:"runningPods,omitempty"`
	// TotalPods is the total number of all pods on the node from the start
	TotalPods int `json:"totalpods,omitempty"`
	// EvictionPods is the total number of pods up for eviction from the start
	EvictionPods int `json:"evictionPods,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// NodeDrain is the Schema for the nodedrains API
type NodeDrain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeDrainSpec   `json:"spec,omitempty"`
	Status NodeDrainStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NodeDrainList contains a list of NodeDrain
type NodeDrainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeDrain `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeDrain{}, &NodeDrainList{})
}
