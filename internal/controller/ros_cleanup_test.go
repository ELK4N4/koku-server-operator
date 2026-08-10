package controller

import (
	"context"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func TestReconcileROSFeature_EnabledKeepsResources(t *testing.T) {
	r, cfg, c := newROSFeatureFixture(t, true)
	ctx := context.Background()

	if err := r.reconcileROSFeature(ctx, cfg); err != nil {
		t.Fatalf("enabled: %v", err)
	}
	assertROSCondition(t, cfg, metav1.ConditionTrue, "Enabled")
	if err := c.Get(ctx, types.NamespacedName{Name: resources.NameKruize(cfg), Namespace: testNamespace}, &appsv1.Deployment{}); err != nil {
		t.Fatalf("kruize should remain while enabled: %v", err)
	}
}

func TestReconcileROSFeature_DisableDeletesResources(t *testing.T) {
	r, cfg, c := newROSFeatureFixture(t, true)
	ctx := context.Background()

	disabled := false
	cfg.Spec.ROS.Enabled = &disabled
	if err := r.reconcileROSFeature(ctx, cfg); err != nil {
		t.Fatalf("disable: %v", err)
	}
	assertROSCondition(t, cfg, metav1.ConditionFalse, "Disabled")
	assertROSObjectsGone(t, ctx, c, cfg)
}

func TestReconcileROSFeature_ReEnableThenDisable(t *testing.T) {
	r, cfg, c := newROSFeatureFixture(t, false)
	ctx := context.Background()

	enabled := true
	cfg.Spec.ROS.Enabled = &enabled
	if err := r.reconcileROSFeature(ctx, cfg); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	assertROSCondition(t, cfg, metav1.ConditionTrue, "Enabled")

	if err := c.Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: resources.NameKruize(cfg), Namespace: testNamespace},
	}); err != nil {
		t.Fatalf("re-create kruize: %v", err)
	}
	if err := c.Create(ctx, &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: resources.NameKruizeClusterRole(cfg)},
	}); err != nil {
		t.Fatalf("re-create clusterrole: %v", err)
	}

	disabled := false
	cfg.Spec.ROS.Enabled = &disabled
	if err := r.reconcileROSFeature(ctx, cfg); err != nil {
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

	if err := r.reconcileROSCleanup(context.Background(), cfg); err != nil {
		t.Fatalf("cleanup with no ROS objects: %v", err)
	}
}

func TestIsIgnorableROSCleanupErr(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "x")
	noMatch := &apimeta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "monitoring.coreos.com", Kind: "ServiceMonitor"}}
	other := fmt.Errorf("permission denied")

	if !isIgnorableROSCleanupErr(notFound) {
		t.Error("NotFound should be ignorable")
	}
	if !isIgnorableROSCleanupErr(noMatch) {
		t.Error("NoKindMatchError should be ignorable")
	}
	if isIgnorableROSCleanupErr(other) {
		t.Error("other errors must not be ignored")
	}
}

func newROSFeatureFixture(t *testing.T, seedObjects bool) (*CostManagementServiceConfigReconciler, *costv1alpha1.CostManagementServiceConfig, client.Client) {
	t.Helper()
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	enabled := true
	cfg.Spec.ROS.Enabled = &enabled

	objs := []client.Object{cfg}
	if seedObjects {
		objs = append(objs,
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: resources.NameKruize(cfg), Namespace: testNamespace}},
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: resources.NameROSAPI(cfg), Namespace: testNamespace}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: resources.NameCdappConfigMap(cfg), Namespace: testNamespace}},
			&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: resources.NameKruizeClusterRole(cfg)}},
			&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: resources.NameKruizeClusterRole(cfg)}},
		)
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(cfg).
		Build()
	r := &CostManagementServiceConfigReconciler{Client: c, Scheme: scheme}
	return r, cfg, c
}

func assertROSCondition(t *testing.T, cfg *costv1alpha1.CostManagementServiceConfig, status metav1.ConditionStatus, reason string) {
	t.Helper()
	cond := apimeta.FindStatusCondition(cfg.Status.Conditions, costv1alpha1.ConditionROSEnabled)
	if cond == nil || cond.Status != status || cond.Reason != reason {
		t.Fatalf("ROSEnabled condition = %+v, want status=%s reason=%s", cond, status, reason)
	}
}

func assertROSObjectsGone(t *testing.T, ctx context.Context, c client.Client, cfg *costv1alpha1.CostManagementServiceConfig) {
	t.Helper()
	for _, key := range []types.NamespacedName{
		{Name: resources.NameKruize(cfg), Namespace: testNamespace},
		{Name: resources.NameROSAPI(cfg), Namespace: testNamespace},
		{Name: resources.NameCdappConfigMap(cfg), Namespace: testNamespace},
	} {
		var err error
		if key.Name == resources.NameCdappConfigMap(cfg) {
			err = c.Get(ctx, key, &corev1.ConfigMap{})
		} else {
			err = c.Get(ctx, key, &appsv1.Deployment{})
		}
		if err == nil {
			t.Errorf("expected %s deleted after disable", key.Name)
		} else if !apierrors.IsNotFound(err) {
			t.Errorf("get %s: %v", key.Name, err)
		}
	}
	if err := c.Get(ctx, types.NamespacedName{Name: resources.NameKruizeClusterRole(cfg)}, &rbacv1.ClusterRole{}); err == nil {
		t.Error("Kruize ClusterRole should be deleted when ROS is disabled")
	}
	if err := c.Get(ctx, types.NamespacedName{Name: resources.NameKruizeClusterRole(cfg)}, &rbacv1.ClusterRoleBinding{}); err == nil {
		t.Error("Kruize ClusterRoleBinding should be deleted when ROS is disabled")
	}
}
