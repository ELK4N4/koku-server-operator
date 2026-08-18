package resources

import (
	"os/exec"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func TestRBACMigrationScriptUsesSeedsOnly(t *testing.T) {
	script := rbacMigrationScript()
	for _, want := range []string{
		"manage.py migrate --noinput",
		"manage.py seeds --skip-notifications",
		"bootstrap_tenants --all",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("rbacMigrationScript missing %q", want)
		}
	}
	for _, absent := range []string{
		"manage.py shell",
		"SEED_SCRIPT",
		"ADMIN_DEFAULT_SCRIPT",
		"CLEANUP_DEFAULTS",
		"Cost Administrator",
	} {
		if strings.Contains(script, absent) {
			t.Errorf("rbacMigrationScript should not contain %q", absent)
		}
	}
}

func TestRBACMigrationJobEnvEnablesSeeding(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-onprem", Namespace: "cost-tests"},
		Spec: costv1alpha1.CostManagementServiceConfigSpec{
			RBAC: costv1alpha1.RBACConfig{
				Image: costv1alpha1.ImageSpec{Repository: "rbac", Tag: "test"},
			},
		},
	}
	job := RBACMigrationJob(cfg, "test")
	if got := job.Annotations["koku.costmanagement.io/image-tag"]; got != "test-cmseed2" {
		t.Errorf("image-tag annotation = %q, want test-cmseed2", got)
	}
	env := map[string]string{}
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	for _, k := range []string{"PERMISSION_SEEDING_ENABLED", "ROLE_SEEDING_ENABLED", "GROUP_SEEDING_ENABLED"} {
		if env[k] != "True" {
			t.Errorf("env %s = %q, want True", k, env[k])
		}
	}
	assertRBACSeedVolumeMounts(t, job.Spec.Template.Spec.Volumes, job.Spec.Template.Spec.Containers[0].VolumeMounts)
}

func TestAdminBootstrapJobGated(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-onprem", Namespace: "cost-tests"},
		Spec: costv1alpha1.CostManagementServiceConfigSpec{
			RBAC: costv1alpha1.RBACConfig{
				Image: costv1alpha1.ImageSpec{Repository: "rbac", Tag: "test"},
			},
		},
	}
	if job := AdminBootstrapJob(cfg, "test"); job != nil {
		t.Fatal("expected nil when bootstrapAdmin.enabled is false")
	}
	cfg.Spec.RBAC.BootstrapAdmin.Enabled = true
	if job := AdminBootstrapJob(cfg, "test"); job != nil {
		t.Fatal("expected nil when secretRef.name is empty")
	}
	cfg.Spec.RBAC.BootstrapAdmin.SecretRef.Name = "rbac-bootstrap-admin"
	job := AdminBootstrapJob(cfg, "test")
	if job == nil {
		t.Fatal("expected AdminBootstrapJob when enabled with secretRef set")
	}
	if job.Name != "cost-onprem-rbac-admin-bootstrap" {
		t.Errorf("name = %q", job.Name)
	}
	script := job.Spec.Template.Spec.Containers[0].Command[2]
	for _, want := range []string{
		"manage.py ensure_user",
		"--application cost-management",
		"--application sources",
		"--admin",
		"--admin-group-name \"Cost Admin Default\"",
		"--admin-policy-name \"Cost Admin Default Policy\"",
		"${SYNC_USERNAME}",
		"${SYNC_ORG_ID}",
		"${SYNC_ACCOUNT_NUMBER}",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("bootstrap script missing %q", want)
		}
	}
	if strings.Contains(script, "manage.py shell") {
		t.Error("bootstrap script must not embed Django ORM via manage.py shell")
	}
	// Identity values must come from the Secret via secretKeyRef — never hardcoded.
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SYNC_ORG_ID" || e.Name == "SYNC_ACCOUNT_NUMBER" || e.Name == "SYNC_USERNAME" {
			if e.Value != "" {
				t.Errorf("env %s has inline value %q — must use secretKeyRef", e.Name, e.Value)
			}
			if e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
				t.Errorf("env %s must use secretKeyRef, got %+v", e.Name, e.ValueFrom)
			}
			if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil &&
				e.ValueFrom.SecretKeyRef.Name != "rbac-bootstrap-admin" {
				t.Errorf("env %s secretKeyRef.name = %q, want rbac-bootstrap-admin",
					e.Name, e.ValueFrom.SecretKeyRef.Name)
			}
		}
	}
	assertRBACSeedVolumeMounts(t, job.Spec.Template.Spec.Volumes, job.Spec.Template.Spec.Containers[0].VolumeMounts)
}

func assertRBACSeedVolumeMounts(t *testing.T, vols []corev1.Volume, mounts []corev1.VolumeMount) {
	t.Helper()
	volNames := map[string]bool{}
	for _, v := range vols {
		volNames[v.Name] = true
	}
	for _, name := range []string{rbacRolePermissionsVolume, rbacRoleDefinitionsVolume} {
		if !volNames[name] {
			t.Errorf("missing volume %q", name)
		}
	}
	mountPaths := map[string]string{}
	for _, m := range mounts {
		mountPaths[m.Name] = m.MountPath
	}
	if mountPaths[rbacRolePermissionsVolume] != rbacRolePermissionsMountPath {
		t.Errorf("permissions mount = %q, want %q", mountPaths[rbacRolePermissionsVolume], rbacRolePermissionsMountPath)
	}
	if mountPaths[rbacRoleDefinitionsVolume] != rbacRoleDefinitionsMountPath {
		t.Errorf("definitions mount = %q, want %q", mountPaths[rbacRoleDefinitionsVolume], rbacRoleDefinitionsMountPath)
	}
}

// TestMigrationScriptsSyntax runs bash -n on every migration script string to
// catch syntax errors (orphaned loop bodies, unclosed heredocs, etc.) that
// pattern-match tests would miss.
func TestMigrationScriptsSyntax(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	scripts := map[string]string{
		"kokuMigrationScript":      kokuMigrationScript(),
		"rosMigrationScript":       rosMigrationScript(),
		"rbacMigrationScript":      rbacMigrationScript(),
		"rbacAdminBootstrapScript": rbacAdminBootstrapScript(),
	}
	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command("bash", "-n", "-c", script)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s has a bash syntax error:\n%s", name, string(out))
			}
		})
	}
}
