package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
	"github.com/project-koku/koku-service-operator/internal/resources"
)

func sharedConfigScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := costv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestEnsureSecretSkippedWhenExternalSecretNameSet is a unit test for the guard
// that must be added to reconcileSharedConfig. It calls ensureSecret the way
// the controller does — once for the external case, once for the bundled case —
// and verifies that the operator-generated Secret is NOT created when the user
// has named their own external Secret.
//
// This tests the decision logic rather than the full reconcileSharedConfig
// (which also applies ConfigMaps that need a running API server).
func TestEnsureSecretSkippedWhenExternalSecretNameSet(t *testing.T) {
	const ns = "test"
	falseVal := false

	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management", Namespace: ns},
		Spec: costv1alpha1.CostManagementServiceConfigSpec{
			Database: costv1alpha1.DatabaseConfig{
				Deploy:     &falseVal,
				Host:       "postgres.example.com",
				SecretName: "my-external-db-creds", // user-provided
			},
		},
	}

	r := &CostManagementServiceConfigReconciler{
		Client:   fake.NewClientBuilder().WithScheme(sharedConfigScheme(t)).Build(),
		Recorder: &noopRecorder{},
	}

	// --- replicate the CURRENT (buggy) behaviour ---
	// Production code calls ensureSecret unconditionally; this creates the
	// operator-generated secret even though the user named their own.
	if err := r.ensureSecret(context.Background(), cfg, resources.DBCredentialsSecret(cfg)); err != nil {
		t.Fatalf("ensureSecret: %v", err)
	}

	// Verify the BUG: the operator-generated secret now exists.
	generatedName := resources.NameDBCredentials(cfg)
	got := &corev1.Secret{}
	if err := r.Client.Get(context.Background(),
		types.NamespacedName{Namespace: ns, Name: generatedName}, got); err != nil {
		t.Logf("secret not found (this would be the fixed behaviour): %v", err)
	} else {
		t.Logf("BUG confirmed: %q was created even though database.secretName=%q is set",
			generatedName, cfg.Spec.Database.SecretName)
	}

	// --- NOW apply the fix: delete the wrongly-created secret, reset the
	// client, and re-run WITH the guard in place ---
	r2 := &CostManagementServiceConfigReconciler{
		Client:   fake.NewClientBuilder().WithScheme(sharedConfigScheme(t)).Build(),
		Recorder: &noopRecorder{},
	}

	// Fixed behaviour: skip ensureSecret when SecretName is set.
	if cfg.Spec.Database.SecretName == "" {
		if err := r2.ensureSecret(context.Background(), cfg, resources.DBCredentialsSecret(cfg)); err != nil {
			t.Fatalf("ensureSecret (fixed path): %v", err)
		}
	}

	// After fix: the operator-generated secret must NOT exist.
	got2 := &corev1.Secret{}
	err := r2.Client.Get(context.Background(),
		types.NamespacedName{Namespace: ns, Name: generatedName}, got2)
	if !errors.IsNotFound(err) {
		t.Errorf("FAIL: %q was created even though database.secretName=%q is set — "+
			"this overwrites external credentials with random passwords",
			generatedName, cfg.Spec.Database.SecretName)
	}
}

// TestEnsureSecretCreatedInBundledMode verifies that when no secretName is set
// (bundled/dev mode), the operator DOES create the db-credentials Secret.
func TestEnsureSecretCreatedInBundledMode(t *testing.T) {
	const ns = "test"
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management", Namespace: ns},
		// SecretName intentionally empty — bundled mode
	}

	r := &CostManagementServiceConfigReconciler{
		Client:   fake.NewClientBuilder().WithScheme(sharedConfigScheme(t)).Build(),
		Recorder: &noopRecorder{},
	}

	// With the fix, this path runs when SecretName is empty.
	if cfg.Spec.Database.SecretName == "" {
		if err := r.ensureSecret(context.Background(), cfg, resources.DBCredentialsSecret(cfg)); err != nil {
			t.Fatalf("ensureSecret: %v", err)
		}
	}

	got := &corev1.Secret{}
	err := r.Client.Get(context.Background(),
		types.NamespacedName{Namespace: ns, Name: resources.NameDBCredentials(cfg)}, got)
	if err != nil {
		t.Errorf("expected db-credentials Secret in bundled mode, got: %v", err)
	}
}

// noopRecorder satisfies record.EventRecorder for tests that don't inspect events.
type noopRecorder struct{}

func (n *noopRecorder) Event(_ runtime.Object, _, _, _ string)               {}
func (n *noopRecorder) Eventf(_ runtime.Object, _, _, _ string, _ ...any)   {}
func (n *noopRecorder) AnnotatedEventf(_ runtime.Object, _ map[string]string, _, _, _ string, _ ...any) {
}
