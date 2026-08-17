package controller

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

// TestMonitoringRealApplyErrorSurfaces verifies that non-CRD-absent errors
// from reconcileMonitoring are returned rather than silently swallowed.
func TestMonitoringRealApplyErrorSurfaces(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = costv1alpha1.AddToScheme(scheme)

	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testCRName, Namespace: "test"},
	}

	realErr := errors.New("etcd is on fire")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
				return realErr
			},
			Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
				return nil // legacy App SM delete is best-effort
			},
		}).
		Build()

	r := &CostManagementServiceConfigReconciler{
		Client:   fakeClient,
		Recorder: &noopRecorder{},
	}

	_, err := r.reconcileMonitoring(context.Background(), cfg)
	if err == nil {
		t.Error("reconcileMonitoring should surface non-CRD-absent apply errors, got nil")
	}
}

// TestMonitoringCRDAbsentSkipsResource verifies IsNoMatchError is treated as success.
func TestMonitoringCRDAbsentSkipsResource(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = costv1alpha1.AddToScheme(scheme)

	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testCRName, Namespace: testNamespace},
	}

	noMatchErr := &apimeta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "monitoring.coreos.com", Kind: "PrometheusRule"}}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
				return noMatchErr
			},
			Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
				return noMatchErr
			},
		}).
		Build()

	r := &CostManagementServiceConfigReconciler{
		Client:   fakeClient,
		Recorder: &noopRecorder{},
	}

	result, err := r.reconcileMonitoring(context.Background(), cfg)
	if err != nil {
		t.Errorf("reconcileMonitoring should skip CRD-absent resources (got error: %v)", err)
	}
	if !result.IsZero() {
		t.Errorf("reconcileMonitoring should return zero result on CRD-absent, got %+v", result)
	}
}

// TestMonitoringAppliesOnlyPrometheusRule ensures PR1 does not Patch ServiceMonitors.
func TestMonitoringAppliesOnlyPrometheusRule(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = costv1alpha1.AddToScheme(scheme)

	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testCRName, Namespace: testNamespace},
	}

	var patchKinds []string
	var deletedAppSM atomic.Bool

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(_ context.Context, _ client.WithWatch, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
				patchKinds = append(patchKinds, obj.GetObjectKind().GroupVersionKind().Kind)
				return nil
			},
			Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
				if obj.GetName() == testCRName+"-app-metrics" {
					deletedAppSM.Store(true)
				}
				return nil
			},
		}).
		Build()

	r := &CostManagementServiceConfigReconciler{
		Client:   fakeClient,
		Recorder: &noopRecorder{},
	}

	if _, err := r.reconcileMonitoring(context.Background(), cfg); err != nil {
		t.Fatalf("reconcileMonitoring: %v", err)
	}
	if !deletedAppSM.Load() {
		t.Error("expected best-effort delete of legacy App ServiceMonitor")
	}
	if len(patchKinds) != 1 || patchKinds[0] != "PrometheusRule" {
		t.Errorf("expected single PrometheusRule patch, got %v", patchKinds)
	}
}

func TestEmitPhaseChanged_OnlyOnChange(t *testing.T) {
	rec := record.NewFakeRecorder(4)
	r := &CostManagementServiceConfigReconciler{Recorder: rec}
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testCRName, Namespace: testNamespace},
	}

	r.emitPhaseChanged(cfg, costv1alpha1.PhaseProgressing, costv1alpha1.PhaseReady)
	assertEvent(t, rec, "PhaseChanged")

	r.emitPhaseChanged(cfg, costv1alpha1.PhaseReady, costv1alpha1.PhaseReady)
	assertNoEvent(t, rec, "PhaseChanged")
}

func TestEmitDependencyFailed_OnlyOnTransition(t *testing.T) {
	rec := record.NewFakeRecorder(4)
	r := &CostManagementServiceConfigReconciler{Recorder: rec}
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testCRName, Namespace: testNamespace},
	}

	r.emitDependencyFailed(cfg, costv1alpha1.ConditionDatabaseReady, "unreachable")
	assertEvent(t, rec, "DependencyFailed")

	r.setCondition(cfg, costv1alpha1.ConditionDatabaseReady, metav1.ConditionFalse, "DatabaseUnreachable", "unreachable")
	r.emitDependencyFailed(cfg, costv1alpha1.ConditionDatabaseReady, "unreachable again")
	assertNoEvent(t, rec, "DependencyFailed")
}

func TestMigrationsCompleteEvent_Reason(t *testing.T) {
	rec := record.NewFakeRecorder(2)
	r := &CostManagementServiceConfigReconciler{Recorder: rec}
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testCRName, Namespace: testNamespace},
	}

	// Simulate completion path: SchemaUpToDate not yet True.
	if !apimeta.IsStatusConditionTrue(cfg.Status.Conditions, costv1alpha1.ConditionSchemaUpToDate) {
		r.Recorder.Event(cfg, "Normal", "MigrationsComplete", "All schema migrations succeeded")
	}
	r.setCondition(cfg, costv1alpha1.ConditionSchemaUpToDate, metav1.ConditionTrue, "MigrationComplete", "ok")
	assertEvent(t, rec, "MigrationsComplete")

	if !apimeta.IsStatusConditionTrue(cfg.Status.Conditions, costv1alpha1.ConditionSchemaUpToDate) {
		r.Recorder.Event(cfg, "Normal", "MigrationsComplete", "should not fire")
	}
	assertNoEvent(t, rec, "MigrationsComplete")
}
