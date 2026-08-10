package resources

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

// TestKruizeClusterRoleDoesNotGrantSecretsAccess verifies that the Kruize
// ClusterRole does not include cluster-wide read access to Secrets.
// Secrets access allows reading every credential in every namespace —
// a significant security over-privilege for a workload optimizer.
func TestKruizeClusterRoleDoesNotGrantSecretsAccess(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management", Namespace: "test"},
	}
	cr := KruizeClusterRole(cfg)

	for _, rule := range cr.Rules {
		for _, res := range rule.Resources {
			if res == "secrets" {
				t.Errorf("KruizeClusterRole grants access to %q — "+
					"this allows reading every Secret cluster-wide; "+
					"remove 'secrets' from the ClusterRole rules", res)
			}
		}
	}
}
