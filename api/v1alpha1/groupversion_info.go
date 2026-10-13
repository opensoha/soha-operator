// Package v1alpha1 contains API Schema definitions for the workloads v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=workloads.soha.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "workloads.soha.io", Version: "v1alpha1"}
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme   = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &WorkloadCronJob{}, &WorkloadCronJobList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
