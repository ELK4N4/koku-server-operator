package controller

import (
	"context"
	"slices"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
	"github.com/project-koku/koku-service-operator/internal/resources"
)

func TestReconcileMigration_FirstReconcileCreatesKokuJob(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.Cache.Deploy = boolPtr(true)

	c := fakeClientWithApplySupport(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	result, err := r.reconcileMigration(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reconcileMigration: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected RequeueAfter while Job is running")
	}

	kokuJobName := resources.NameKokuMigration(cfg)
	if !jobExists(t, c, testNamespace, kokuJobName) {
		t.Fatalf("expected Koku migration Job %q to exist", kokuJobName)
	}
	if getJobAnnotation(t, c, testNamespace, kokuJobName, "koku.costmanagement.io/image-tag") != cfg.Spec.CostManagement.API.Image.Tag {
		t.Errorf("Koku Job missing image-tag annotation")
	}

	// RBAC and ROS jobs should NOT exist yet (sequential creation)
	if jobExists(t, c, testNamespace, resources.NameRBACMigration(cfg)) {
		t.Fatal("expected RBAC Job to NOT exist on first pass (sequential)")
	}
	if jobExists(t, c, testNamespace, resources.NameROSMigration(cfg)) {
		t.Fatal("expected ROS Job to NOT exist on first pass (sequential)")
	}

	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionSchemaUpToDate)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "MigrationRunning" {
		t.Fatalf("expected SchemaUpToDate=False MigrationRunning, got %+v", cond)
	}
}

func TestReconcileMigration_KokuComplete_CreatesRBACJob(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.Cache.Deploy = boolPtr(true)
	cfg.Spec.RBAC.Image.Tag = "rbac-tag"

	c := fakeClientWithApplySupport(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	kokuJobName := resources.NameKokuMigration(cfg)
	markJobComplete(t, c, testNamespace, kokuJobName)

	result, err := r.reconcileMigration(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected RequeueAfter while RBAC Job is running")
	}

	rbacJobName := resources.NameRBACMigration(cfg)
	if !jobExists(t, c, testNamespace, rbacJobName) {
		t.Fatalf("expected RBAC migration Job %q to exist", rbacJobName)
	}
	if getJobAnnotation(t, c, testNamespace, rbacJobName, "koku.costmanagement.io/image-tag") != "rbac-tag-cmseed1" {
		t.Errorf("RBAC Job image-tag should include cmseed1 suffix")
	}

	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionSchemaUpToDate)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "MigrationRunning" {
		t.Fatalf("expected SchemaUpToDate=False MigrationRunning (RBAC), got %+v", cond)
	}
}

func TestReconcileMigration_AllComplete_SchemaUpToDateTrue(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.Cache.Deploy = boolPtr(true)

	c := fakeClientWithApplySupport(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	steps := []string{
		resources.NameKokuMigration(cfg),
		resources.NameRBACMigration(cfg),
	}
	for _, jobName := range steps {
		if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
			t.Fatalf("step for %s: %v", jobName, err)
		}
		markJobComplete(t, c, testNamespace, jobName)
	}

	result, err := r.reconcileMigration(context.Background(), cfg)
	if err != nil {
		t.Fatalf("final pass: %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected zero Result when all migrations complete, got %+v", result)
	}

	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionSchemaUpToDate)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != "MigrationComplete" {
		t.Fatalf("expected SchemaUpToDate=True MigrationComplete, got %+v", cond)
	}

	// Degraded should be cleared on success (controller.go:411 sets Degraded=False MigrationSucceeded)
	degraded := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionDegraded)
	if degraded == nil || degraded.Status != metav1.ConditionFalse || degraded.Reason != "MigrationSucceeded" {
		t.Fatalf("expected Degraded=False MigrationSucceeded, got %+v", degraded)
	}
}

func TestReconcileMigration_JobFailed_DegradedAndStop(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.Cache.Deploy = boolPtr(true)

	c := fakeClientWithApplySupport(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
		t.Fatalf("create: %v", err)
	}
	kokuJobName := resources.NameKokuMigration(cfg)
	markJobFailed(t, c, testNamespace, kokuJobName)

	result, err := r.reconcileMigration(context.Background(), cfg)
	if err != nil {
		t.Fatalf("after failure: %v", err)
	}
	if !result.Stop {
		t.Fatal("expected Stop=true when Job fails")
	}

	// RBAC Job should NOT have been created (pipeline stops on failure)
	if jobExists(t, c, testNamespace, resources.NameRBACMigration(cfg)) {
		t.Fatal("expected RBAC Job to NOT exist after Koku failure (pipeline stops)")
	}

	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionSchemaUpToDate)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "MigrationFailed" {
		t.Fatalf("expected SchemaUpToDate=False MigrationFailed, got %+v", cond)
	}
	if !apimeta.IsStatusConditionTrue(cfg.Status.Conditions, costv1alpha1.ConditionDegraded) {
		t.Fatal("expected Degraded=True on migration failure")
	}
	degraded := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionDegraded)
	if degraded.Reason != "MigrationFailed" {
		t.Errorf("Degraded reason = %q, want MigrationFailed", degraded.Reason)
	}
}

func TestReconcileMigration_ImageTagChange_RecreatesJob(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.Cache.Deploy = boolPtr(true)
	cfg.Spec.CostManagement.API.Image.Tag = "v1"

	c := fakeClientWithApplySupport(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
		t.Fatalf("v1 create: %v", err)
	}
	kokuJobName := resources.NameKokuMigration(cfg)
	markJobComplete(t, c, testNamespace, kokuJobName)

	cfg.Spec.CostManagement.API.Image.Tag = "v2"
	result, err := r.reconcileMigration(context.Background(), cfg)
	if err != nil {
		t.Fatalf("v2 reconcile (delete): %v", err)
	}

	// Old Job should be deleted
	if jobExists(t, c, testNamespace, kokuJobName) {
		t.Fatal("expected old Job to be deleted on image tag change")
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected RequeueAfter after deleting stale Job")
	}

	// Next reconcile should create new Job with v2 tag
	result, err = r.reconcileMigration(context.Background(), cfg)
	if err != nil {
		t.Fatalf("v2 reconcile (recreate): %v", err)
	}
	if !jobExists(t, c, testNamespace, kokuJobName) {
		t.Fatal("expected new Job to be created on requeue")
	}
	newAnnotation := getJobAnnotation(t, c, testNamespace, kokuJobName, "koku.costmanagement.io/image-tag")
	if newAnnotation != "v2" {
		t.Errorf("expected Job recreated with v2 annotation, got %q", newAnnotation)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected RequeueAfter for new Job")
	}
}

func TestReconcileMigration_ROSDisabled_SkipsROSMigration(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.Cache.Deploy = boolPtr(true)
	cfg.Spec.ROS.Enabled = boolPtr(false)

	c := fakeClientWithApplySupport(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
		t.Fatalf("koku: %v", err)
	}
	markJobComplete(t, c, testNamespace, resources.NameKokuMigration(cfg))

	if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
		t.Fatalf("rbac: %v", err)
	}

	if jobExists(t, c, testNamespace, resources.NameROSMigration(cfg)) {
		t.Fatal("expected no ROS MigrationJob when ROS disabled")
	}

	if countJobs(t, c, testNamespace) != 2 {
		t.Errorf("expected 2 jobs (Koku + RBAC), got %d", countJobs(t, c, testNamespace))
	}
}

func TestReconcileMigration_ROSEnabled_IncludesROSMigration(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.Cache.Deploy = boolPtr(true)
	cfg.Spec.ROS.Enabled = boolPtr(true)

	c := fakeClientWithApplySupport(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
		t.Fatalf("koku: %v", err)
	}
	markJobComplete(t, c, testNamespace, resources.NameKokuMigration(cfg))

	if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
		t.Fatalf("ros: %v", err)
	}
	if !jobExists(t, c, testNamespace, resources.NameROSMigration(cfg)) {
		t.Fatal("expected ROS MigrationJob when ROS enabled")
	}
}

func TestReconcileMigration_AdminBootstrapGated(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.Cache.Deploy = boolPtr(true)

	c := fakeClientWithApplySupport(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	for _, jobName := range []string{
		resources.NameKokuMigration(cfg),
		resources.NameRBACMigration(cfg),
	} {
		if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
			t.Fatalf("step: %v", err)
		}
		markJobComplete(t, c, testNamespace, jobName)
	}

	result, err := r.reconcileMigration(context.Background(), cfg)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected zero result, got %+v", result)
	}
	if jobExists(t, c, testNamespace, resources.NameRBACAdminBootstrap(cfg)) {
		t.Fatal("expected no AdminBootstrap Job when disabled")
	}
}

func TestReconcileMigration_AdminBootstrapEnabledWithSecret_CreatesJob(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.Cache.Deploy = boolPtr(true)
	cfg.Spec.RBAC.BootstrapAdmin.Enabled = true
	cfg.Spec.RBAC.BootstrapAdmin.SecretRef.Name = "rbac-bootstrap-admin"

	c := fakeClientWithApplySupport(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	for _, jobName := range []string{
		resources.NameKokuMigration(cfg),
		resources.NameRBACMigration(cfg),
	} {
		if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
			t.Fatalf("step: %v", err)
		}
		markJobComplete(t, c, testNamespace, jobName)
	}

	if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !jobExists(t, c, testNamespace, resources.NameRBACAdminBootstrap(cfg)) {
		t.Fatal("expected AdminBootstrap Job when enabled with secretRef")
	}
}

func TestReconcileMigration_AdminBootstrapEnabledNoSecret_WarningEvent(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.Database.Deploy = boolPtr(true)
	cfg.Spec.Cache.Deploy = boolPtr(true)
	cfg.Spec.RBAC.BootstrapAdmin.Enabled = true

	c := fakeClientWithApplySupport(scheme)
	recorder := &testRecorder{Events: make([]string, 0)}
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: recorder,
	}

	for _, jobName := range []string{
		resources.NameKokuMigration(cfg),
		resources.NameRBACMigration(cfg),
	} {
		if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
			t.Fatalf("step: %v", err)
		}
		markJobComplete(t, c, testNamespace, jobName)
	}

	if _, err := r.reconcileMigration(context.Background(), cfg); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if !recorder.hasEvent("BootstrapAdminSkipped") {
		t.Fatal("expected BootstrapAdminSkipped warning event")
	}
	if jobExists(t, c, testNamespace, resources.NameRBACAdminBootstrap(cfg)) {
		t.Fatal("expected no AdminBootstrap Job when secretRef empty")
	}
}

func markJobComplete(t *testing.T, c client.Client, ns, name string) {
	t.Helper()
	ctx := context.Background()
	job := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, job); err != nil {
		t.Fatalf("get job %s: %v", name, err)
	}
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if err := c.Status().Update(ctx, job); err != nil {
		t.Fatalf("mark job complete: %v", err)
	}
}

func markJobFailed(t *testing.T, c client.Client, ns, name string) {
	t.Helper()
	ctx := context.Background()
	job := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, job); err != nil {
		t.Fatalf("get job %s: %v", name, err)
	}
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	if err := c.Status().Update(ctx, job); err != nil {
		t.Fatalf("mark job failed: %v", err)
	}
}

func getJobAnnotation(t *testing.T, c client.Client, ns, name, key string) string {
	t.Helper()
	ctx := context.Background()
	job := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, job); err != nil {
		t.Fatalf("get job %s: %v", name, err)
	}
	return job.Annotations[key]
}

func jobExists(t *testing.T, c client.Client, ns, name string) bool {
	ctx := context.Background()
	job := &batchv1.Job{}
	err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, job)
	return err == nil
}

func countJobs(t *testing.T, c client.Client, ns string) int {
	ctx := context.Background()
	list := &batchv1.JobList{}
	if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	return len(list.Items)
}

type testRecorder struct {
	Events []string
}

func (t *testRecorder) Event(obj runtime.Object, eventtype, reason, message string) {
	t.Events = append(t.Events, reason)
}
func (t *testRecorder) Eventf(obj runtime.Object, eventtype, reason, message string, args ...any) {
	t.Events = append(t.Events, reason)
}
func (t *testRecorder) AnnotatedEventf(obj runtime.Object, annotations map[string]string, eventtype, reason, message string, args ...any) {
	t.Events = append(t.Events, reason)
}
func (t *testRecorder) hasEvent(reason string) bool {
	return slices.Contains(t.Events, reason)
}
