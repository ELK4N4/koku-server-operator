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
	for _, rule := range cr.Rules {
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
				t.Errorf("objectbucketclaims rule must not include verb %q: %+v", v, rule)
			}
		}
		if !hasGet || !hasList {
			t.Errorf("objectbucketclaims rule missing get and/or list: %+v", rule)
			continue
		}
		found = true
	}
	if !found {
		t.Fatal("manager-role must grant get+list on objectbucket.io/objectbucketclaims")
	}

	var clusterCR rbacv1.ClusterRole
	decodeYAMLFile(t, rbacManifestPath(t, "cluster_access_role.yaml"), &clusterCR)
	for _, rule := range clusterCR.Rules {
		if slices.Contains(rule.APIGroups, wantGroup) {
			t.Errorf("manager-cluster-role must not grant objectbucket.io: %+v", rule)
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
