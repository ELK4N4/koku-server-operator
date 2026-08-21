package controller

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	routeKind         = "Route"
	routeAdmittedType = "Admitted"
	routeStatusTrue   = "True"
)

// routeAdmitted reports whether an OpenShift Route has been admitted by a router.
// Requires status.ingress to be non-empty. An ingress entry is admitted when it
// has conditions with type=Admitted status=True, or — if that entry has no
// conditions — when host is non-empty.
func routeAdmitted(u *unstructured.Unstructured) bool {
	if u == nil {
		return false
	}
	ingress, found, err := unstructured.NestedSlice(u.Object, "status", "ingress")
	if err != nil || !found || len(ingress) == 0 {
		return false
	}
	for _, item := range ingress {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		conds, condsFound, condErr := unstructured.NestedSlice(m, "conditions")
		if condErr != nil {
			continue
		}
		if !condsFound || len(conds) == 0 {
			host, _, _ := unstructured.NestedString(m, "host")
			if host != "" {
				return true
			}
			continue
		}
		for _, c := range conds {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			typ, _, _ := unstructured.NestedString(cm, "type")
			status, _, _ := unstructured.NestedString(cm, "status")
			if typ == routeAdmittedType && status == routeStatusTrue {
				return true
			}
		}
	}
	return false
}
