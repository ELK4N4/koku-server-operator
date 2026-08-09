// Package v1alpha1 contains API Schema definitions for the service.costmanagement.openshift.io group.
// +kubebuilder:object:generate=true
// +groupName=service.costmanagement.openshift.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "service.costmanagement.openshift.io", Version: "v1alpha1"}
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme   = SchemeBuilder.AddToScheme
)
