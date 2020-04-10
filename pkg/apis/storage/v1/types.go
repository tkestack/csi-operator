/*
 * Tencent is pleased to support the open source community by making TKEStack available.
 *
 * Copyright (C) 2012-2019 Tencent. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use
 * this file except in compliance with the License. You may obtain a copy of the
 * License at
 *
 * https://opensource.org/licenses/Apache-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 * WARRANTIES OF ANY KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations under the License.
 */

package v1

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CSISpec defines the desired state of CSI
type CSISpec struct {
	// Version can be set to a well known CSI version.
	// If version set, you need to set DriverName to a well known driver type,
	// and left DriverTemplate, Node, Controller as empty. The operator will
	// enhance these fields.
	Version CSIVersion `json:"version" protobuf:"bytes,1,opt,name=version"`
	// Parameters contains global parameters for a well known CSI type and version.
	// Such as ceph cluster information, etc.
	Parameters map[string]string `json:"parameters" protobuf:"bytes,2,opt,name=parameters"`
	// Name of the CSI driver.
	DriverName string `json:"driverName" protobuf:"bytes,3,opt,name=driverName"`
	// Driver info.
	DriverTemplate *CSIDriverTemplate `json:"driverTemplate" protobuf:"bytes,4,opt,name=driverTemplate"`
	// Components info of daemonSet sidecars.
	// +optional
	Node CSINode `json:"node" protobuf:"bytes,5,opt,name=node"`
	// Components info of controller sidecars.
	// +optional
	Controller CSIController `json:"controller" protobuf:"bytes,6,opt,name=controller"`
	// Secrets used to provision/attach/resize/snapshot.
	// +optional
	Secrets []corev1.Secret `json:"secrets,omitempty" protobuf:"bytes,7,opt,name=secrets"`
	// StorageClasses relevant to the Driver. Note that the provisioner name will
	// be override by the name of driver.
	// +optional
	StorageClasses []storagev1.StorageClass `json:"storageClasses,omitempty" protobuf:"bytes,8,opt,name=storageClasses"`
}

const (
	// CSIDriverCephRBD indicates the CephRBD storage type.
	CSIDriverCephRBD = "csi-rbd"
	// CSIDriverCephFS indicates the CephFS storage type.
	CSIDriverCephFS = "csi-cephfs"
	// CSIDriverTencentCBS indicates the Tencent Cloud CBS storage type.
	CSIDriverTencentCBS = "com.tencent.cloud.csi.cbs"
	// CSIDriverTencentCOS indicates the Tencent Cloud COS storage type.
	CSIDriverTencentCOS = "csi-tencent-cloud-cos"
	// CSIDriverTencentCFS indicates the Tencent Cloud CFS storage type.
	CSIDriverTencentCFS = "com.tencent.cloud.csi.cfs"
)

// CSIVersion indicates the version of CSI external components.
type CSIVersion string

const (
	// CSIVersionV0 indicates the 0.3.0 version of CSI.
	CSIVersionV0 = "v0"
	// CSIVersionV1 indicates the 1.x version of CSI.
	CSIVersionV1 = "v1"
)

// CSIDriverTemplate is the definition of the Driver container.
type CSIDriverTemplate struct {
	// Should contain one and only one container which is the concrete driver.
	// The container should use the CSI_ENDPOINT env to get the CSI socket in the command.
	// It should only contain CSI un-related volumes and volumeMounts, such as /sys, /lib/modules, etc.
	Template corev1.PodTemplateSpec `json:"template,omitempty" protobuf:"bytes,1,opt,name=template"`
	// Special Cluster rules needed by the driver.
	// +optional
	Rules []rbacv1.PolicyRule `json:"rules,omitempty" protobuf:"bytes,2,opt,name=rules"`
}

// CSIController is the configuration of the controller sidecars.
type CSIController struct {
	// Replicas of the controller deployment.
	Replicas int32 `json:"replicas" protobuf:"bytes,1,opt,name=replicas"`
	// Configuration for CSI Provisioner.
	// +optional
	Provisioner *CSIComponent `json:"provisioner" protobuf:"bytes,2,opt,name=provisioner"`
	// Configuration for CSI Attacher.
	// +optional
	Attacher *CSIComponent `json:"attacher" protobuf:"bytes,3,opt,name=attacher"`
	// Configuration for CSI Resizer.
	// +optional
	Resizer *CSIComponent `json:"resizer" protobuf:"bytes,4,opt,name=resizer"`
	// Configuration for CSI Snapshotter.
	// +optional
	Snapshotter *CSIComponent `json:"snapshotter" protobuf:"bytes,5,opt,name=snapshotter"`
	// Configuration for CSI ClusterRegister.
	// +optional
	ClusterRegistrar *CSIComponent `json:"clusterRegistrar" protobuf:"bytes,6,opt,name=clusterRegistrar"`
	// Configuration for CSI LivenessProbe.
	// +optional
	LivenessProbe *CSIComponent `json:"livenessProbe" protobuf:"bytes,7,opt,name=livenessProbe"`
}

// CSINode is the configuration of the node sidecars.
type CSINode struct {
	// Configuration for CSI NodeRegistrar.
	// +optional
	NodeRegistrar *CSIComponent `json:"nodeRegistrar" protobuf:"bytes,1,opt,name=nodeRegistrar"`
	// Configuration for CSI LivenessProbe.
	// +optional
	LivenessProbe *CSIComponent `json:"livenessProbe" protobuf:"bytes,2,opt,name=livenessProbe"`
}

// CSIComponent is the basic configuration of a external component.
type CSIComponent struct {
	// Name of the Controller sidecar container image.
	// +optional
	Image string `json:"image" protobuf:"bytes,1,opt,name=image"`
	// Resources for the controller container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources" protobuf:"bytes,2,opt,name=resources"`
	// Parameters contains additional parameters for external components.
	// +optional
	Parameters map[string]string `json:"parameters" protobuf:"bytes,3,opt,name=parameters"`
}

// CSIPhase indicates the status of a CSI object.
type CSIPhase string

const (
	// CSIPending indicates the CSI components is creating.
	CSIPending = "Pending"
	// CSIRunning indicates the CSI components is running.
	CSIRunning = "Running"
	// CSIFailed indicates creating CSI components failed.
	CSIFailed = "Failed"
)

// CSIStatus defines the observed state of CSI
type CSIStatus struct {
	// The status of the CSI object.
	Phase CSIPhase `json:"phase,omitempty" protobuf:"bytes,1,opt,name=phase"`

	// The generation observed by the operator.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"bytes,2,opt,name=observedGeneration"`

	// Generation of Driver DaemonSets and Controller Deployments that the operator has created / updated.
	Children []Generation `json:"children,omitempty" protobuf:"bytes,3,opt,name=children"`

	// Represents the latest available observations of a CSI's current state.
	Conditions []CSICondition `json:"conditions,omitempty" protobuf:"bytes,4,opt,name=conditions"`
}

// Generation keeps track of the generation for a given object.
type Generation struct {
	// Group is the group of the object the operator involved
	Group string `json:"group"`
	// Kind is the resource type of the object the operator involved
	Kind string `json:"resource"`
	// Namespace is where the object the operator involved
	Namespace string `json:"namespace"`
	// Name is the name of the object the operator involved
	Name string `json:"name"`
	// LastGeneration is the last generation of the object the operator involved
	LastGeneration int64 `json:"lastGeneration"`
}

// CSICondition describes the state of a CSI at a certain point.
type CSICondition struct {
	// Type of deployment condition.
	Type string `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// Last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition.
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CSI is the Schema for the csis API
// +k8s:openapi-gen=true
type CSI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CSISpec   `json:"spec,omitempty"`
	Status CSIStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CSIList contains a list of CSI
type CSIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CSI `json:"items"`
}

// init func.
func init() {
	SchemeBuilder.Register(&CSI{}, &CSIList{})
}
