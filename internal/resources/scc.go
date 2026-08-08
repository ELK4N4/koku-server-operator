package resources

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-server-operator/api/v1alpha1"
)

// OpenShiftAnyUIDRoleBinding grants the OpenShift anyuid SCC to a ServiceAccount.
// Required so ubi-minimal init containers with an explicit non-root UID (1001)
// can schedule under restricted namespaces.
func OpenShiftAnyUIDRoleBinding(cfg *costv1alpha1.CostManagementServiceConfig, saName, component string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "RoleBinding"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName + "-anyuid",
			Namespace: cfg.Namespace,
			Labels:    Labels(cfg, component),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      saName,
			Namespace: cfg.Namespace,
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "system:openshift:scc:anyuid",
		},
	}
}
