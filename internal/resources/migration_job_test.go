package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func minimalCRForResources(name, ns string) *costv1alpha1.CostManagementServiceConfig {
	return &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       "test-uid-1234",
		},
	}
}

func TestMigrationJobFields(t *testing.T) {
	cfg := minimalCRForResources("test", "ns")
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.CostManagement.API.Image.Tag = "v1"

	job := MigrationJob(cfg, "v1")

	if *job.Spec.BackoffLimit != 3 {
		t.Errorf("BackoffLimit = %d, want 3", *job.Spec.BackoffLimit)
	}
	if *job.Spec.ActiveDeadlineSeconds != 600 {
		t.Errorf("ActiveDeadlineSeconds = %d, want 600", *job.Spec.ActiveDeadlineSeconds)
	}
	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyOnFailure {
		t.Errorf("RestartPolicy = %s, want OnFailure", job.Spec.Template.Spec.RestartPolicy)
	}

	if job.Annotations["koku.costmanagement.io/image-tag"] != "v1" {
		t.Errorf("image-tag annotation = %q, want v1", job.Annotations["koku.costmanagement.io/image-tag"])
	}

	if job.Spec.TTLSecondsAfterFinished != nil {
		t.Errorf("TTLSecondsAfterFinished should be nil, got %d", *job.Spec.TTLSecondsAfterFinished)
	}

	if len(job.Spec.Template.Spec.InitContainers) != 2 {
		t.Errorf("expected 2 init containers, got %d", len(job.Spec.Template.Spec.InitContainers))
	}
}

func TestMigrationJobNames(t *testing.T) {
	cfg := minimalCRForResources("cost-onprem", "cost-tests")

	tests := map[string]string{
		"Koku":           "cost-onprem-koku-migrate",
		"ROS":            "cost-onprem-ros-migrate",
		"RBAC":           "cost-onprem-rbac-migrate",
		"AdminBootstrap": "cost-onprem-rbac-admin-bootstrap",
	}

	for name, want := range tests {
		var got string
		switch name {
		case "Koku":
			got = NameKokuMigration(cfg)
		case "ROS":
			got = NameROSMigration(cfg)
		case "RBAC":
			got = NameRBACMigration(cfg)
		case "AdminBootstrap":
			got = NameRBACAdminBootstrap(cfg)
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
}

func TestRBACSeedTagFormat(t *testing.T) {
	if RBACSeedJobTag("v1.2.3") != "v1.2.3-cmseed1" {
		t.Errorf("RBACSeedJobTag format wrong")
	}
}

func TestMigrationJob_ServiceAccountName(t *testing.T) {
	cfg := minimalCRForResources("test", "ns")
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.CostManagement.API.Image.Tag = "v1"

	job := MigrationJob(cfg, "v1")
	if job.Spec.Template.Spec.ServiceAccountName != NameKokuServiceAccount(cfg) {
		t.Errorf("ServiceAccountName = %q, want %q", job.Spec.Template.Spec.ServiceAccountName, NameKokuServiceAccount(cfg))
	}
}

func TestMigrationJob_NonRootPodSecurityContext(t *testing.T) {
	cfg := minimalCRForResources("test", "ns")
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.CostManagement.API.Image.Tag = "v1"

	job := MigrationJob(cfg, "v1")
	if job.Spec.Template.Spec.SecurityContext == nil {
		t.Fatal("expected pod SecurityContext")
	}
	if job.Spec.Template.Spec.SecurityContext.RunAsNonRoot == nil || !*job.Spec.Template.Spec.SecurityContext.RunAsNonRoot {
		t.Error("expected RunAsNonRoot=true")
	}
}

func TestMigrationJob_ContainerResources(t *testing.T) {
	cfg := minimalCRForResources("test", "ns")
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.CostManagement.API.Image.Tag = "v1"

	job := MigrationJob(cfg, "v1")
	container := job.Spec.Template.Spec.Containers[0]

	cpuReq := container.Resources.Requests[corev1.ResourceCPU]
	memReq := container.Resources.Requests[corev1.ResourceMemory]
	cpuLim := container.Resources.Limits[corev1.ResourceCPU]
	memLim := container.Resources.Limits[corev1.ResourceMemory]

	if cpuReq.String() != "250m" {
		t.Errorf("CPU request = %s, want 250m", cpuReq.String())
	}
	if memReq.String() != "512Mi" {
		t.Errorf("Memory request = %s, want 512Mi", memReq.String())
	}
	if cpuLim.String() != "500m" {
		t.Errorf("CPU limit = %s, want 500m", cpuLim.String())
	}
	if memLim.String() != "1Gi" {
		t.Errorf("Memory limit = %s, want 1Gi", memLim.String())
	}
}

func TestMigrationJob_ContainerSecurityContext(t *testing.T) {
	cfg := minimalCRForResources("test", "ns")
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.CostManagement.API.Image.Tag = "v1"

	job := MigrationJob(cfg, "v1")
	container := job.Spec.Template.Spec.Containers[0]

	if container.SecurityContext == nil {
		t.Fatal("expected container SecurityContext")
	}
	if container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Error("expected AllowPrivilegeEscalation=false")
	}
	if container.SecurityContext.RunAsNonRoot == nil || !*container.SecurityContext.RunAsNonRoot {
		t.Error("expected RunAsNonRoot=true")
	}
	if container.SecurityContext.Capabilities == nil || len(container.SecurityContext.Capabilities.Drop) == 0 {
		t.Error("expected Capabilities.Drop to include ALL")
	}
}

func TestROSMigrationJob_UsesRosDBName(t *testing.T) {
	cfg := minimalCRForResources("test", "ns")
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.Database.Port = 5432

	job := ROSMigrationJob(cfg, "ros-tag")

	env := make(map[string]string)
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}

	if env["DB_NAME"] != RosDBName {
		t.Errorf("DB_NAME = %s, want %s", env["DB_NAME"], RosDBName)
	}
}

func TestRBACMigrationJob_EnablesSeedingFlags(t *testing.T) {
	cfg := minimalCRForResources("test", "ns")
	cfg.Spec.Database.Deploy = boolPtr(true)

	job := RBACMigrationJob(cfg, "rbac-tag")

	env := make(map[string]string)
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}

	for _, k := range []string{"PERMISSION_SEEDING_ENABLED", "ROLE_SEEDING_ENABLED", "GROUP_SEEDING_ENABLED"} {
		if env[k] != "True" {
			t.Errorf("env %s = %q, want True", k, env[k])
		}
	}
}

func TestAdminBootstrapJob_GatedByEnabledAndSecretRef(t *testing.T) {
	cfg := minimalCRForResources("test", "ns")
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.RBAC.Image.Tag = "rbac-tag"

	if job := AdminBootstrapJob(cfg, "rbac-tag"); job != nil {
		t.Fatal("expected nil when bootstrapAdmin.enabled is false")
	}

	cfg.Spec.RBAC.BootstrapAdmin.Enabled = true
	if job := AdminBootstrapJob(cfg, "rbac-tag"); job != nil {
		t.Fatal("expected nil when secretRef.name is empty")
	}

	cfg.Spec.RBAC.BootstrapAdmin.SecretRef.Name = "rbac-bootstrap-admin"
	job := AdminBootstrapJob(cfg, "rbac-tag")
	if job == nil {
		t.Fatal("expected AdminBootstrapJob when enabled with secretRef set")
	}
	if job.Name != "test-rbac-admin-bootstrap" {
		t.Errorf("name = %q", job.Name)
	}

	env := make(map[string]string)
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			env[e.Name] = e.ValueFrom.SecretKeyRef.Key
		}
	}

	for _, k := range []string{"SYNC_ORG_ID", "SYNC_ACCOUNT_NUMBER", "SYNC_USERNAME"} {
		if env[k] == "" {
			t.Errorf("env %s must use secretKeyRef", k)
		}
	}
	if env["SYNC_ORG_ID"] != "org-id" || env["SYNC_ACCOUNT_NUMBER"] != "account-number" || env["SYNC_USERNAME"] != "username" {
		t.Errorf("secretKeyRef keys incorrect: %+v", env)
	}
}