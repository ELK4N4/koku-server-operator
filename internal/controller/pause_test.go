package controller

import (
	"context"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func TestIsPaused(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{name: "nil annotations", want: false},
		{name: "missing key", annotations: map[string]string{}, want: false},
		{name: "true", annotations: map[string]string{pauseAnnotation: annotationTrue}, want: true},
		{name: "TRUE case-insensitive", annotations: map[string]string{pauseAnnotation: "TRUE"}, want: true},
		{name: "true with spaces", annotations: map[string]string{pauseAnnotation: " true "}, want: true},
		{name: "false", annotations: map[string]string{pauseAnnotation: "false"}, want: false},
		{name: "empty value", annotations: map[string]string{pauseAnnotation: ""}, want: false},
		{name: "other value", annotations: map[string]string{pauseAnnotation: "1"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &costv1alpha1.CostManagementServiceConfig{}
			cfg.Annotations = tc.annotations
			if got := isPaused(cfg); got != tc.want {
				t.Errorf("isPaused() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReconcile_PausedSkipsPhases(t *testing.T) {
	cr := minimalCR(testCRName, testNamespace)
	controllerutil.AddFinalizer(cr, finalizerName)
	cr.Annotations = map[string]string{pauseAnnotation: annotationTrue}
	// Seed a Ready-ish status so we can assert pause does not wipe Available.
	cr.Status.Phase = costv1alpha1.PhaseReady
	apimeta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
		Type:   costv1alpha1.ConditionAvailable,
		Status: metav1.ConditionTrue,
		Reason: "Ready",
	})

	c := fake.NewClientBuilder().
		WithScheme(ownershipScheme(t)).
		WithObjects(cr).
		WithStatusSubresource(cr).
		Build()

	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   ownershipScheme(t),
		Recorder: record.NewFakeRecorder(10),
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: testCRName, Namespace: testNamespace},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("expected zero Result while paused, got %+v", result)
	}

	updated := &costv1alpha1.CostManagementServiceConfig{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: testCRName, Namespace: testNamespace}, updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}

	if !apimeta.IsStatusConditionTrue(updated.Status.Conditions, costv1alpha1.ConditionPaused) {
		t.Error("expected Paused=True")
	}
	prog := apimeta.FindStatusCondition(updated.Status.Conditions, costv1alpha1.ConditionProgressing)
	if prog == nil || prog.Status != metav1.ConditionFalse || prog.Reason != "Paused" {
		t.Errorf("expected Progressing=False reason=Paused, got %#v", prog)
	}
	if !apimeta.IsStatusConditionTrue(updated.Status.Conditions, costv1alpha1.ConditionAvailable) {
		t.Error("pause must not clear Available")
	}
	if updated.Status.Phase != costv1alpha1.PhaseReady {
		t.Errorf("pause must not rewrite Phase; got %q", updated.Status.Phase)
	}
	// Discovery never ran — no DiscoveryComplete from the phase pipeline.
	if apimeta.FindStatusCondition(updated.Status.Conditions, costv1alpha1.ConditionDiscoveryComplete) != nil {
		t.Error("paused reconcile must not run discovery phase")
	}
}

func TestReconcile_ResumeClearsPausedCondition(t *testing.T) {
	cr := minimalCR(testCRName, testNamespace)
	controllerutil.AddFinalizer(cr, finalizerName)
	// No pause annotation — reconciliation should run (and fail discovery early
	// in this minimal fake cluster). Prior Paused condition must be cleared.
	apimeta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
		Type:   costv1alpha1.ConditionPaused,
		Status: metav1.ConditionTrue,
		Reason: "AnnotationSet",
	})

	c := fake.NewClientBuilder().
		WithScheme(ownershipScheme(t)).
		WithObjects(cr).
		WithStatusSubresource(cr).
		Build()

	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   ownershipScheme(t),
		Recorder: record.NewFakeRecorder(10),
	}

	_, _ = r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: testCRName, Namespace: testNamespace},
	})

	updated := &costv1alpha1.CostManagementServiceConfig{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: testCRName, Namespace: testNamespace}, updated); err != nil {
		t.Fatalf("get CR: %v", err)
	}

	paused := apimeta.FindStatusCondition(updated.Status.Conditions, costv1alpha1.ConditionPaused)
	if paused == nil {
		t.Fatal("expected Paused condition to still exist (False)")
	}
	if paused.Status != metav1.ConditionFalse || paused.Reason != "Resumed" {
		t.Errorf("expected Paused=False reason=Resumed, got status=%s reason=%s", paused.Status, paused.Reason)
	}
}
