package controller

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func rbacManifestPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "config", "rbac", name)
}

func bundleCSVPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "bundle", "manifests", "koku-service-operator.clusterserviceversion.yaml")
}

func decodeYAMLFile(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := yaml.Unmarshal(data, into); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// olmCSVInstallPermissions is the CSV fragment we round-trip. Avoids adding
// operator-framework/api just to inspect install.spec.{permissions,clusterPermissions}.
type olmCSVInstallPermissions struct {
	Spec struct {
		Install struct {
			Spec struct {
				ClusterPermissions []olmCSVPermissionRules `json:"clusterPermissions"`
				Permissions        []olmCSVPermissionRules `json:"permissions"`
			} `json:"spec"`
		} `json:"install"`
	} `json:"spec"`
}

type olmCSVPermissionRules struct {
	Rules []rbacv1.PolicyRule `json:"rules"`
}

func csvPolicyRules(perms []olmCSVPermissionRules) []rbacv1.PolicyRule {
	var rules []rbacv1.PolicyRule
	for _, p := range perms {
		rules = append(rules, p.Rules...)
	}
	return rules
}

func assertObjectBucketClaimGetList(t *testing.T, source string, rules []rbacv1.PolicyRule) {
	t.Helper()
	const (
		wantGroup    = "objectbucket.io"
		wantResource = "objectbucketclaims"
	)
	extraVerbs := map[string]struct{}{
		"watch":  {},
		"create": {},
		"update": {},
		"patch":  {},
		"delete": {},
	}

	var found bool
	for _, rule := range rules {
		if !slices.Contains(rule.APIGroups, wantGroup) {
			continue
		}
		if !slices.Contains(rule.Resources, wantResource) {
			continue
		}

		hasGet, hasList := false, false
		for _, v := range rule.Verbs {
			switch v {
			case "get":
				hasGet = true
			case "list":
				hasList = true
			}
			if _, extra := extraVerbs[v]; extra {
				t.Errorf("%s objectbucketclaims rule must not include verb %q: %+v", source, v, rule)
			}
		}
		if !hasGet || !hasList {
			t.Errorf("%s objectbucketclaims rule missing get and/or list: %+v", source, rule)
			continue
		}
		found = true
	}
	if !found {
		t.Fatalf("%s must grant get+list on objectbucket.io/objectbucketclaims", source)
	}
}

func TestManagerRoleBinding_IsNamespacedRoleBinding(t *testing.T) {
	var rb rbacv1.RoleBinding
	decodeYAMLFile(t, rbacManifestPath(t, "role_binding.yaml"), &rb)
	if rb.Kind != "RoleBinding" {
		t.Fatalf("manager binding kind: got %q, want RoleBinding (OwnNamespace)", rb.Kind)
	}
	if rb.RoleRef.Kind != "ClusterRole" || rb.RoleRef.Name != "manager-role" {
		t.Fatalf("roleRef: got %+v, want ClusterRole/manager-role", rb.RoleRef)
	}
}

func TestClusterAccessRole_NarrowNooBaaSecretGet(t *testing.T) {
	var cr rbacv1.ClusterRole
	decodeYAMLFile(t, rbacManifestPath(t, "cluster_access_role.yaml"), &cr)
	if cr.Name != "manager-cluster-role" {
		t.Fatalf("cluster access role name: got %q", cr.Name)
	}

	var foundNoobaa bool
	for _, rule := range cr.Rules {
		for _, res := range rule.Resources {
			if res != "secrets" {
				continue
			}
			// Blanket secrets list/watch must not appear on the cluster role.
			for _, v := range rule.Verbs {
				if v == "list" || v == "watch" || v == "create" || v == "delete" || v == "update" || v == "patch" {
					t.Errorf("cluster access secrets rule must not include verb %q: %+v", v, rule)
				}
			}
			if len(rule.ResourceNames) == 1 && rule.ResourceNames[0] == "noobaa-admin" {
				foundNoobaa = true
				hasGet := false
				for _, v := range rule.Verbs {
					if v == "get" {
						hasGet = true
					}
				}
				if !hasGet {
					t.Errorf("noobaa-admin rule missing get: %+v", rule)
				}
			} else if len(rule.ResourceNames) == 0 {
				t.Errorf("cluster access must not grant unnamed secrets: %+v", rule)
			}
		}
	}
	if !foundNoobaa {
		t.Fatal("expected secrets get with resourceNames=[noobaa-admin] on manager-cluster-role")
	}
}

func TestManagerRole_GrantsObjectBucketClaimGetList(t *testing.T) {
	var cr rbacv1.ClusterRole
	decodeYAMLFile(t, rbacManifestPath(t, "role.yaml"), &cr)
	if cr.Name != "manager-role" {
		t.Fatalf("manager role name: got %q", cr.Name)
	}
	assertObjectBucketClaimGetList(t, "manager-role", cr.Rules)

	var clusterCR rbacv1.ClusterRole
	decodeYAMLFile(t, rbacManifestPath(t, "cluster_access_role.yaml"), &clusterCR)
	for _, rule := range clusterCR.Rules {
		if slices.Contains(rule.APIGroups, "objectbucket.io") {
			t.Errorf("manager-cluster-role must not grant objectbucket.io: %+v", rule)
		}
	}

	// OLM installs from the CSV, not role.yaml. CI does not regenerate the
	// bundle, so lock namespaced permissions to the same get+list grant.
	// ObjectBucketClaim is namespace-scoped, so the grant belongs in
	// permissions (OwnNamespace), not clusterPermissions.
	var csv olmCSVInstallPermissions
	decodeYAMLFile(t, bundleCSVPath(t), &csv)
	if len(csv.Spec.Install.Spec.Permissions) == 0 {
		t.Fatal("CSV spec.install.spec.permissions is empty (unmarshal failed or field moved)")
	}
	if len(csv.Spec.Install.Spec.ClusterPermissions) == 0 {
		t.Fatal("CSV spec.install.spec.clusterPermissions is empty (unmarshal failed or field moved)")
	}
	assertObjectBucketClaimGetList(t, "CSV permissions", csvPolicyRules(csv.Spec.Install.Spec.Permissions))
	for _, rule := range csvPolicyRules(csv.Spec.Install.Spec.ClusterPermissions) {
		if slices.Contains(rule.APIGroups, "objectbucket.io") {
			t.Errorf("CSV clusterPermissions must not grant objectbucket.io: %+v", rule)
		}
	}
}

func TestManagerRole_StillGrantsNamespacedSecrets(t *testing.T) {
	var cr rbacv1.ClusterRole
	decodeYAMLFile(t, rbacManifestPath(t, "role.yaml"), &cr)
	if cr.Name != "manager-role" {
		t.Fatalf("manager role name: got %q", cr.Name)
	}
	var foundSecrets bool
	for _, rule := range cr.Rules {
		for _, res := range rule.Resources {
			if res == "secrets" && len(rule.ResourceNames) == 0 {
				foundSecrets = true
			}
		}
	}
	if !foundSecrets {
		t.Fatal("manager-role must still list unnamed secrets (scoped by RoleBinding)")
	}
}

// clusterScopedResources belong in cluster_access_role.yaml. role.yaml is
// bound via a namespaced RoleBinding, so rules for these resources are inert
// today but would regain cluster-wide reach if the binding were switched
// back to a ClusterRoleBinding (review follow-up #6).
var clusterScopedResources = map[string]struct{}{
	"consolelinks":        {},
	"clusterroles":        {},
	"clusterrolebindings": {},
	"storageclasses":      {},
}

func assertNoClusterScopedResources(t *testing.T, source string, rules []rbacv1.PolicyRule) {
	t.Helper()
	for _, rule := range rules {
		for _, res := range rule.Resources {
			if _, forbidden := clusterScopedResources[res]; forbidden {
				t.Errorf("%s must not grant cluster-scoped resource %q (belongs in cluster_access_role.yaml): %+v", source, res, rule)
			}
			// OpenShift Ingress/cluster is cluster-scoped; networking.k8s.io
			// Ingress is namespaced and would be fine in role.yaml.
			if res == "ingresses" && slices.Contains(rule.APIGroups, "config.openshift.io") {
				t.Errorf("%s must not grant config.openshift.io/ingresses (belongs in cluster_access_role.yaml): %+v", source, rule)
			}
		}
	}
}

func TestManagerRole_NoClusterScopedResources(t *testing.T) {
	var cr rbacv1.ClusterRole
	decodeYAMLFile(t, rbacManifestPath(t, "role.yaml"), &cr)
	if cr.Name != "manager-role" {
		t.Fatalf("manager role name: got %q", cr.Name)
	}
	assertNoClusterScopedResources(t, "manager-role", cr.Rules)

	// OLM installs from the CSV, not role.yaml. Lock namespaced permissions
	// to the same constraint so a regenerated bundle cannot reintroduce
	// cluster-scoped rules under OwnNamespace.
	var csv olmCSVInstallPermissions
	decodeYAMLFile(t, bundleCSVPath(t), &csv)
	if len(csv.Spec.Install.Spec.Permissions) == 0 {
		t.Fatal("CSV spec.install.spec.permissions is empty (unmarshal failed or field moved)")
	}
	assertNoClusterScopedResources(t, "CSV permissions", csvPolicyRules(csv.Spec.Install.Spec.Permissions))
}
