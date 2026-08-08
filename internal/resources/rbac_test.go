package resources

import (
	"testing"

	costv1alpha1 "github.com/project-koku/koku-server-operator/api/v1alpha1"
)

func TestRBACEnvAPIPathPrefix(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{}
	cfg.Name = "cost-onprem"
	cfg.Namespace = "cost-tests"

	var got string
	for _, e := range rbacEnv(cfg) {
		if e.Name == "API_PATH_PREFIX" {
			got = e.Value
			break
		}
	}
	// Must match cost-onprem-chart cost-onprem.rbac.apiPathPrefix (/api/rbac).
	if got != "/api/rbac" {
		t.Fatalf("API_PATH_PREFIX = %q, want /api/rbac", got)
	}
}
