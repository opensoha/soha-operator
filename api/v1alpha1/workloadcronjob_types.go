package v1alpha1

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkloadKind identifies a supported source workload.
// +kubebuilder:validation:Enum=Deployment;StatefulSet;DaemonSet
type WorkloadKind string

const (
	WorkloadKindDeployment  WorkloadKind = "Deployment"
	WorkloadKindStatefulSet WorkloadKind = "StatefulSet"
	WorkloadKindDaemonSet   WorkloadKind = "DaemonSet"
)

// WorkloadReference selects one regular container from a same-namespace workload.
type WorkloadReference struct {
	Kind WorkloadKind `json:"kind"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Container string `json:"container"`
}

// WorkloadCronJobSpec defines the desired CronJob and source container runtime fields it follows.
type WorkloadCronJobSpec struct {
	SourceRef WorkloadReference `json:"sourceRef"`

	// TargetContainer is the CronJob container whose image, environment, and mounts follow SourceRef.Container.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	TargetContainer string `json:"targetContainer"`

	CronJobSpec batchv1.CronJobSpec `json:"cronJobSpec"`
}

// WorkloadCronJobStatus records the source and owned CronJob observed by the controller.
type WorkloadCronJobStatus struct {
	ObservedGeneration    int64                   `json:"observedGeneration,omitempty"`
	SourceUID             string                  `json:"sourceUid,omitempty"`
	SourceResourceVersion string                  `json:"sourceResourceVersion,omitempty"`
	SourceImage           string                  `json:"sourceImage,omitempty"`
	CronJobRef            *corev1.ObjectReference `json:"cronJobRef,omitempty"`
	LastSyncTime          *metav1.Time            `json:"lastSyncTime,omitempty"`
	Conditions            []metav1.Condition      `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=wcj,categories=soha
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceRef.name`
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.status.sourceImage`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// WorkloadCronJob owns a native CronJob and keeps one target container runtime aligned with a source workload.
type WorkloadCronJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkloadCronJobSpec   `json:"spec"`
	Status WorkloadCronJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkloadCronJobList contains a list of WorkloadCronJob resources.
type WorkloadCronJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkloadCronJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkloadCronJob{}, &WorkloadCronJobList{})
}
