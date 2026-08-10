package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
	"github.com/project-koku/koku-service-operator/internal/resources"
)

func TestROSCleanupObjects_IncludesClusterScoped(t *testing.T) {
	cfg := minimalCR(testCRName, testNamespace)
	names := map[string]bool{}
	for _, obj := range rosCleanupObjects(cfg) {
		names[obj.GetName()] = true
	}
	for _, want := range []string{
		resources.NameKruizeClusterRole(cfg),
		resources.NameKruize(cfg),
		resources.NameROSAPI(cfg),
		resources.NameCdappConfigMap(cfg),
		resources.NameROSMigration(cfg),
		cfg.Name + "-kruize-delete-partitions",
		cfg.Name + "-ros-partition-cleaner",
		cfg.Name + "-kruize-metrics",
	} {
		if !names[want] {
			t.Errorf("rosCleanupObjects missing %q", want)
		}
	}
}

func TestReconcileROSFeature_ToggleLifecycle(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	enabled := true
	cfg.Spec.ROS.Enabled = &enabled

	kruizeDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: resources.NameKruize(cfg), Namespace: testNamespace},
	}
	rosAPI := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: resources.NameROSAPI(cfg), Namespace: testNamespace},
	}
	cdapp := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: resources.NameCdappConfigMap(cfg), Namespace: testNamespace},
	}
	kruizeCR := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: resources.NameKruizeClusterRole(cfg)},
	}
	kruizeCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: resources.NameKruizeClusterRole(cfg)},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cfg, kruizeDep, rosAPI, cdapp, kruizeCR, kruizeCRB).
		WithStatusSubresource(cfg).
		Build()
	r := &CostManagementServiceConfigReconciler{Client: c, Scheme: scheme}
	ctx := context.Background()

	// 1. Enabled: resources remain; condition True.
	if _, err := r.reconcileROSFeature(ctx, cfg); err != nil {
		t.Fatalf("enabled: %v", err)
	}
	cond := apimeta.FindStatusCondition(cfg.Status.Conditions, costv1alpha1.ConditionROSEnabled)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != "Enabled" {
		t.Fatalf("enabled condition = %+v", cond)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: resources.NameKruize(cfg), Namespace: testNamespace}, &appsv1.Deployment{}); err != nil {
		t.Fatalf("kruize should remain while enabled: %v", err)
	}

	// 2. Disable: cleanup deletes ROS/Kruize objects; condition False.
	disabled := false
	cfg.Spec.ROS.Enabled = &disabled
	if _, err := r.reconcileROSFeature(ctx, cfg); err != nil {
		t.Fatalf("disable: %v", err)
	}
	cond = apimeta.FindStatusCondition(cfg.Status.Conditions, costv1alpha1.ConditionROSEnabled)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "Disabled" {
		t.Fatalf("disabled condition = %+v", cond)
	}
	for _, key := range []types.NamespacedName{
		{Name: resources.NameKruize(cfg), Namespace: testNamespace},
		{Name: resources.NameROSAPI(cfg), Namespace: testNamespace},
		{Name: resources.NameCdappConfigMap(cfg), Namespace: testNamespace},
	} {
		var dep appsv1.Deployment
		var cm corev1.ConfigMap
		var err error
		if key.Name == resources.NameCdappConfigMap(cfg) {
			err = c.Get(ctx, key, &cm)
		} else {
			err = c.Get(ctx, key, &dep)
		}
		if err == nil {
			t.Errorf("expected %s deleted after disable", key.Name)
		} else if !errors.IsNotFound(err) {
			t.Errorf("get %s: %v", key.Name, err)
		}
	}
	if err := c.Get(ctx, types.NamespacedName{Name: resources.NameKruizeClusterRole(cfg)}, &rbacv1.ClusterRole{}); err == nil {
		t.Error("Kruize ClusterRole should be deleted when ROS is disabled")
	}
	if err := c.Get(ctx, types.NamespacedName{Name: resources.NameKruizeClusterRole(cfg)}, &rbacv1.ClusterRoleBinding{}); err == nil {
		t.Error("Kruize ClusterRoleBinding should be deleted when ROS is disabled")
	}

	// 3. Re-enable: condition True; simulate core/workers re-applying resources.
	cfg.Spec.ROS.Enabled = &enabled
	if _, err := r.reconcileROSFeature(ctx, cfg); err != nil {
		t.Fatalf("re-enable feature gate: %v", err)
	}
	cond = apimeta.FindStatusCondition(cfg.Status.Conditions, costv1alpha1.ConditionROSEnabled)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("re-enable condition = %+v", cond)
	}
	if err := c.Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: resources.NameKruize(cfg), Namespace: testNamespace},
	}); err != nil {
		t.Fatalf("re-create kruize after enable: %v", err)
	}
	if err := c.Create(ctx, &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: resources.NameKruizeClusterRole(cfg)},
	}); err != nil {
		t.Fatalf("re-create clusterrole: %v", err)
	}

	// 4. Disable again: re-created objects are cleaned up.
	cfg.Spec.ROS.Enabled = &disabled
	if _, err := r.reconcileROSFeature(ctx, cfg); err != nil {
		t.Fatalf("second disable: %v", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: resources.NameKruize(cfg), Namespace: testNamespace}, &appsv1.Deployment{}); err == nil {
		t.Error("kruize should be deleted on second disable")
	}
	if err := c.Get(ctx, types.NamespacedName{Name: resources.NameKruizeClusterRole(cfg)}, &rbacv1.ClusterRole{}); err == nil {
		t.Error("ClusterRole should be deleted on second disable")
	}
}

func TestReconcileROSCleanup_ToleratesMissing(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()
	r := &CostManagementServiceConfigReconciler{Client: c, Scheme: scheme}

	if _, err := r.reconcileROSCleanup(context.Background(), cfg); err != nil {
		t.Fatalf("cleanup with no ROS objects: %v", err)
	}
}
