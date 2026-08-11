package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func TestKokuCommonEnvS3CredentialNames(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management", Namespace: "cost-onprem"},
		Spec: costv1alpha1.CostManagementServiceConfigSpec{
			ObjectStorage: costv1alpha1.ObjectStorageConfig{
				SecretName: "user-s3-creds",
			},
		},
	}
	storageSecret := NameStorageSecret(cfg)
	if storageSecret != "user-s3-creds" {
		t.Fatalf("NameStorageSecret = %q, want user-s3-creds", storageSecret)
	}

	env := KokuCommonEnv(cfg)
	byName := make(map[string]corev1.EnvVar, len(env))
	for _, e := range env {
		if _, dup := byName[e.Name]; dup {
			t.Fatalf("duplicate env var %q", e.Name)
		}
		byName[e.Name] = e
	}

	// Contract: koku EnvConfigurator reads S3_ACCESS_KEY / S3_SECRET into
	// settings.S3_*; AWS_* remain for the boto3 default credential chain.
	want := map[string]string{
		"S3_ACCESS_KEY":        "access-key",
		"S3_SECRET":            "secret-key",
		"AWS_ACCESS_KEY_ID":    "access-key",
		"AWS_SECRET_ACCESS_KEY": "secret-key",
	}
	for name, wantKey := range want {
		e, ok := byName[name]
		if !ok {
			t.Fatalf("missing env var %s", name)
		}
		if e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
			t.Fatalf("%s: expected secretKeyRef, got %#v", name, e)
		}
		ref := e.ValueFrom.SecretKeyRef
		if ref.Name != storageSecret {
			t.Errorf("%s secret name: got %q, want %q", name, ref.Name, storageSecret)
		}
		if ref.Key != wantKey {
			t.Errorf("%s secret key: got %q, want %q", name, ref.Key, wantKey)
		}
		if ref.Optional == nil || !*ref.Optional {
			t.Errorf("%s: expected Optional=true secretKeyRef", name)
		}
	}
}

func TestKokuWorkloadsCarryS3CredentialEnv(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management", Namespace: "cost-onprem"},
	}

	containers := [][]corev1.Container{
		KokuAPIDeployment(cfg).Spec.Template.Spec.Containers,
		MasuDeployment(cfg).Spec.Template.Spec.Containers,
		ListenerDeployment(cfg).Spec.Template.Spec.Containers,
		CeleryBeatDeployment(cfg).Spec.Template.Spec.Containers,
		CeleryWorkerDeployment(cfg, "celery", costv1alpha1.CeleryWorkerSpec{Replicas: 1}).Spec.Template.Spec.Containers,
		MigrationJob(cfg, "latest").Spec.Template.Spec.Containers,
	}
	for i, pods := range containers {
		found := false
		for _, c := range pods {
			for _, e := range c.Env {
				if e.Name == "S3_ACCESS_KEY" {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("workload %d missing S3_ACCESS_KEY in container env", i)
		}
	}
}

func TestMergeEnvStableOrder(t *testing.T) {
	overrides := map[string]string{
		"Z_LAST":  "z",
		"A_FIRST": "a",
		"M_MID":   "m",
	}
	var first []string
	for i := range 20 {
		merged := MergeEnv(nil, overrides)
		names := make([]string, len(merged))
		for j, e := range merged {
			names[j] = e.Name
		}
		if i == 0 {
			first = names
			continue
		}
		for j := range names {
			if names[j] != first[j] {
				t.Fatalf("unstable env order on iteration %d: got %v want %v", i, names, first)
			}
		}
	}
	want := []string{"A_FIRST", "M_MID", "Z_LAST"}
	for i, name := range want {
		if first[i] != name {
			t.Fatalf("expected sorted keys %v, got %v", want, first)
		}
	}
}

func TestMergeEnvOverrideReplacesBase(t *testing.T) {
	base := []corev1.EnvVar{
		EnvVal("REDIS_HOST", "operator-default"),
		EnvVal("DB_HOST", "keep-this"),
	}
	overrides := map[string]string{
		"REDIS_HOST": "user-override",
		"NEW_VAR":    "new-value",
	}

	merged := MergeEnv(base, overrides)

	vals := make(map[string]string, len(merged))
	for _, e := range merged {
		if _, dup := vals[e.Name]; dup {
			t.Fatalf("duplicate env var %q", e.Name)
		}
		vals[e.Name] = e.Value
	}

	if vals["REDIS_HOST"] != "user-override" {
		t.Fatalf("REDIS_HOST: got %q, want %q", vals["REDIS_HOST"], "user-override")
	}
	if vals["DB_HOST"] != "keep-this" {
		t.Fatalf("DB_HOST: got %q, want %q", vals["DB_HOST"], "keep-this")
	}
	if vals["NEW_VAR"] != "new-value" {
		t.Fatalf("NEW_VAR: got %q, want %q", vals["NEW_VAR"], "new-value")
	}
	if len(merged) != 3 {
		t.Fatalf("expected 3 env vars, got %d", len(merged))
	}
}
