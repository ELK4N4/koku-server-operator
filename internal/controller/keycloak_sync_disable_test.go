package controller

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
	"github.com/project-koku/koku-service-operator/internal/resources"
)

// TestKeycloakSyncDeletedWhenDisabled verifies that toggling
// spec.rbac.keycloakSync.enabled from true to false causes the reconciler
// to delete the CronJob and ConfigMap that were created when the feature
// was enabled.
func TestKeycloakSyncDeletedWhenDisabled(t *testing.T) {
	const ns = "test"

	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testCRName, Namespace: ns},
		Spec: costv1alpha1.CostManagementServiceConfigSpec{
			RBAC: costv1alpha1.RBACConfig{
				KeycloakSync: costv1alpha1.KeycloakSyncSpec{
					Enabled: false, // disabled
				},
			},
		},
	}

	existingCJ := resources.KeycloakSyncCronJob(cfg)
	existingCJ.Namespace = ns
	existingCM := resources.KeycloakSyncConfigMap(cfg)
	existingCM.Namespace = ns

	r := &CostManagementServiceConfigReconciler{
		Client:   fake.NewClientBuilder().WithScheme(cronJobScheme(t)).WithObjects(cfg, existingCJ, existingCM).Build(),
		Recorder: &noopRecorder{},
	}

	_, _ = r.reconcileWorkers(context.Background(), cfg)

	cj := &batchv1.CronJob{}
	if err := r.Get(context.Background(),
		types.NamespacedName{Namespace: ns, Name: existingCJ.Name}, cj); err == nil {
		t.Errorf("CronJob %q still exists after keycloakSync was disabled", existingCJ.Name)
	}

	cm := &corev1.ConfigMap{}
	if err := r.Get(context.Background(),
		types.NamespacedName{Namespace: ns, Name: existingCM.Name}, cm); err == nil {
		t.Errorf("ConfigMap %q still exists after keycloakSync was disabled", existingCM.Name)
	}
}
