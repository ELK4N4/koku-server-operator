package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	costv1alpha1 "github.com/project-koku/koku-server-operator/api/v1alpha1"
	"github.com/project-koku/koku-server-operator/internal/resources"
)

const (
	fieldOwner    = "koku-server-operator"
	finalizerName = "cost.redhat.com/cleanup"
	requeueFast   = 10 * time.Second
	requeueSlow   = 30 * time.Second
	requeueDrift  = 5 * time.Minute
)

type CostManagementServiceConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=costmanagement-service-cfg.openshift.io,resources=costmanagementserviceconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=costmanagement-service-cfg.openshift.io,resources=costmanagementserviceconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=costmanagement-service-cfg.openshift.io,resources=costmanagementserviceconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs;cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services;configmaps;secrets;serviceaccounts;persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes/custom-host,verbs=create
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.openshift.io,resources=ingresses,verbs=get
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete

func (r *CostManagementServiceConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	cfg := &costv1alpha1.CostManagementServiceConfig{}
	if err := r.Get(ctx, req.NamespacedName, cfg); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Deletion path: clean up cluster-scoped resources then remove finalizer.
	if !cfg.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cfg)
	}

	// Ensure our finalizer is registered before touching any cluster-scoped resource.
	if !controllerutil.ContainsFinalizer(cfg, finalizerName) {
		controllerutil.AddFinalizer(cfg, finalizerName)
		if err := r.Update(ctx, cfg); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{}, nil // requeue triggered by the Update
	}

	original := cfg.DeepCopy()

	result, reconcileErr := r.reconcile(ctx, cfg)

	if patchErr := r.patchStatus(ctx, original, cfg); patchErr != nil {
		logger.Error(patchErr, "failed to patch status")
		if reconcileErr == nil {
			return result, patchErr
		}
	}

	return result, reconcileErr
}

// reconcileDelete removes cluster-scoped resources that cannot be cleaned up via
// ownerReferences, then strips the finalizer so the CR can be fully deleted.
func (r *CostManagementServiceConfigReconciler) reconcileDelete(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("deleting cluster-scoped resources before CR removal")

	// All cluster-scoped resources the operator creates, keyed by type.
	// Add entries here whenever a new cluster-scoped resource type is introduced.
	clusterScoped := []client.Object{
		resources.KruizeClusterRoleBinding(cfg),
		resources.KruizeClusterRole(cfg),
		// Future: ConsoleLink for UI (COST-7690), RBAC ClusterRole (COST-7689)
	}

	for _, obj := range clusterScoped {
		if err := r.Delete(ctx, obj); err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("deleting %s %s: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
		}
	}

	controllerutil.RemoveFinalizer(cfg, finalizerName)
	if err := r.Update(ctx, cfg); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// reconcile drives the ordered, staged rollout:
//  1. Shared configuration (ConfigMaps, Secrets, ServiceAccount)
//  2. Infrastructure (PostgreSQL, Valkey)
//  3. DB migration gate
//  4. Core services (Koku API, Masu, Listener)
//  5. Workers (Celery, ROS, RBAC, Kruize, Ingress)
//  6. Edge (Envoy gateway, UI, Route)
func (r *CostManagementServiceConfigReconciler) reconcile(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig) (ctrl.Result, error) {
	cfg.Status.ObservedGeneration = cfg.Generation
	cfg.Status.Phase = costv1alpha1.PhaseProgressing
	r.setCondition(cfg, costv1alpha1.ConditionProgressing, metav1.ConditionTrue, "Reconciling", "Reconciliation in progress")

	result, err := runPhases([]PhaseFn{
		func() (Result, error) { return r.reconcileDiscovery(ctx, cfg) },
		func() (Result, error) { return r.reconcileSharedConfig(ctx, cfg) },
		func() (Result, error) { return r.reconcileInfrastructure(ctx, cfg) },
		func() (Result, error) { return r.reconcileMigration(ctx, cfg) },
		func() (Result, error) { return r.reconcileCoreServices(ctx, cfg) },
		func() (Result, error) { return r.reconcileWorkers(ctx, cfg) },
		func() (Result, error) { return r.reconcileEdge(ctx, cfg) },
	})

	if err != nil {
		applyPhaseError(cfg, err)
		return ctrl.Result{RequeueAfter: requeueSlow}, err
	}
	if !result.IsZero() {
		return ctrl.Result{RequeueAfter: result.RequeueAfter}, nil
	}

	r.setCondition(cfg, costv1alpha1.ConditionAvailable, metav1.ConditionTrue, "AllComponentsReady", "All components are running")
	r.setCondition(cfg, costv1alpha1.ConditionProgressing, metav1.ConditionFalse, "ReconcileComplete", "")
	cfg.Status.Phase = costv1alpha1.PhaseReady
	// Periodic drift correction: re-apply all desired state every 5 minutes so
	// manual edits to managed resources are reverted without waiting for an event.
	return ctrl.Result{RequeueAfter: requeueDrift}, nil
}

// -----------------------------------------------------------------------------
// Stage 1 — Shared configuration objects
// -----------------------------------------------------------------------------

func (r *CostManagementServiceConfigReconciler) reconcileSharedConfig(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig) (Result, error) {
	// Secrets — create-if-absent only (never overwrite credentials).
	if err := r.ensureSecret(ctx, cfg, resources.DBCredentialsSecret(cfg)); err != nil {
		return Result{}, fmt.Errorf("db-credentials secret: %w", err)
	}
	if err := r.ensureSecret(ctx, cfg, resources.DjangoSecret(cfg)); err != nil {
		return Result{}, fmt.Errorf("django secret: %w", err)
	}
	if cfg.Spec.ObjectStorage.SecretName == "" {
		if err := r.ensureSecret(ctx, cfg, resources.StorageCredentialsSecret(cfg)); err != nil {
			return Result{}, fmt.Errorf("storage credentials secret: %w", err)
		}
	}

	// ConfigMaps
	for _, cm := range []*corev1.ConfigMap{
		resources.DBInitConfigMap(cfg),
		resources.AWSConfigMap(cfg),
		resources.CACombineConfigMap(cfg),
		resources.ServiceCAConfigMap(cfg),
	} {
		if err := r.apply(ctx, cfg, cm); err != nil {
			return Result{}, fmt.Errorf("configmap %s: %w", cm.Name, err)
		}
	}

	// ServiceAccount
	if err := r.apply(ctx, cfg, resources.KokuServiceAccount(cfg)); err != nil {
		return Result{}, fmt.Errorf("koku serviceaccount: %w", err)
	}

	return Result{}, nil
}

// -----------------------------------------------------------------------------
// Stage 2 — Infrastructure
// -----------------------------------------------------------------------------

func (r *CostManagementServiceConfigReconciler) reconcileInfrastructure(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig) (Result, error) {
	if costv1alpha1.BoolVal(cfg.Spec.Database.Deploy, true) {
		if err := r.apply(ctx, cfg, resources.DatabaseService(cfg)); err != nil {
			return Result{}, fmt.Errorf("database service: %w", err)
		}
		if err := r.applyStatefulSet(ctx, cfg, resources.DatabaseStatefulSet(cfg)); err != nil {
			return Result{}, fmt.Errorf("database statefulset: %w", err)
		}
		// Gate: wait for the DB pod to be ready.
		ready, err := r.isStatefulSetReady(ctx, cfg.Namespace, resources.NameDatabase(cfg))
		if err != nil {
			return Result{}, err
		}
		if !ready {
			r.setCondition(cfg, costv1alpha1.ConditionDatabaseReady, metav1.ConditionFalse, "WaitingForDatabase", "waiting for PostgreSQL pod")
			return Result{RequeueAfter: requeueFast}, nil
		}
		r.setCondition(cfg, costv1alpha1.ConditionDatabaseReady, metav1.ConditionTrue, "DatabaseAvailable", "")
	} else {
		r.setCondition(cfg, costv1alpha1.ConditionDatabaseReady, metav1.ConditionTrue, "ExternalDatabase", "")
	}

	if costv1alpha1.BoolVal(cfg.Spec.Cache.Deploy, true) {
		if err := r.apply(ctx, cfg, resources.CachePVC(cfg)); err != nil {
			return Result{}, fmt.Errorf("valkey pvc: %w", err)
		}
		if err := r.apply(ctx, cfg, resources.CacheDeployment(cfg)); err != nil {
			return Result{}, fmt.Errorf("valkey deployment: %w", err)
		}
		if err := r.apply(ctx, cfg, resources.CacheService(cfg)); err != nil {
			return Result{}, fmt.Errorf("valkey service: %w", err)
		}
		ready, err := r.isDeploymentReady(ctx, cfg.Namespace, resources.NameValkey(cfg))
		if err != nil {
			return Result{}, err
		}
		if !ready {
			r.setCondition(cfg, costv1alpha1.ConditionCacheReady, metav1.ConditionFalse, "WaitingForCache", "waiting for Valkey pod")
			return Result{RequeueAfter: requeueFast}, nil
		}
		r.setCondition(cfg, costv1alpha1.ConditionCacheReady, metav1.ConditionTrue, "CacheAvailable", "")
	} else {
		r.setCondition(cfg, costv1alpha1.ConditionCacheReady, metav1.ConditionTrue, "ExternalCache", "")
	}

	return Result{}, nil
}

// -----------------------------------------------------------------------------
// Stage 3 — DB migration gate
// -----------------------------------------------------------------------------

func (r *CostManagementServiceConfigReconciler) reconcileMigration(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig) (Result, error) {
	imageTag := cfg.Spec.CostManagement.API.Image.Tag
	jobName := resources.NameKokuMigration(cfg)

	existing := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Namespace: cfg.Namespace, Name: jobName}, existing)

	if errors.IsNotFound(err) {
		// First run: create the Job.
		job := resources.MigrationJob(cfg, imageTag)
		setOwnerRef(cfg, job)
		if createErr := r.Create(ctx, job); createErr != nil {
			return Result{}, fmt.Errorf("create migration job: %w", createErr)
		}
		r.setCondition(cfg, costv1alpha1.ConditionSchemaUpToDate, metav1.ConditionFalse, "MigrationRunning", "migration job created")
		return Result{RequeueAfter: requeueFast}, nil
	}
	if err != nil {
		return Result{}, err
	}

	// Upgrade detection: if the image tag changed, delete the old Job so it
	// will be re-created with the new image on the next reconcile.
	if existing.Annotations["koku.costmanagement.io/image-tag"] != imageTag {
		if delErr := r.Delete(ctx, existing, client.PropagationPolicy(metav1.DeletePropagationBackground)); delErr != nil && !errors.IsNotFound(delErr) {
			return Result{}, fmt.Errorf("delete stale migration job: %w", delErr)
		}
		r.setCondition(cfg, costv1alpha1.ConditionSchemaUpToDate, metav1.ConditionFalse, "MigrationRunning", "restarting migration for new image")
		return Result{RequeueAfter: requeueFast}, nil
	}

	// Check completion.
	if isJobComplete(existing) {
		r.setCondition(cfg, costv1alpha1.ConditionSchemaUpToDate, metav1.ConditionTrue, "MigrationComplete", "")
		r.setCondition(cfg, costv1alpha1.ConditionDegraded, metav1.ConditionFalse, "MigrationSucceeded", "")
		return Result{}, nil
	}
	if isJobFailed(existing) {
		r.setCondition(cfg, costv1alpha1.ConditionSchemaUpToDate, metav1.ConditionFalse, "MigrationFailed", "migration job failed — check pod logs")
		r.setCondition(cfg, costv1alpha1.ConditionDegraded, metav1.ConditionTrue, "MigrationFailed", "Database migration job failed")
		cfg.Status.Phase = costv1alpha1.PhaseDegraded
		// Stop the pipeline; do not proceed to core services.
		return Result{Stop: true}, nil
	}

	r.setCondition(cfg, costv1alpha1.ConditionSchemaUpToDate, metav1.ConditionFalse, "MigrationRunning", "migration running")
	return Result{RequeueAfter: requeueFast}, nil
}

// -----------------------------------------------------------------------------
// Stage 4 — Core services
// -----------------------------------------------------------------------------

func (r *CostManagementServiceConfigReconciler) reconcileCoreServices(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig) (Result, error) {
	// Kruize (needed by ROS Processor and Poller — deploy before ROS).
	kruizeObjs := []client.Object{
		resources.KruizeServiceAccount(cfg),
		resources.KruizeConfigMap(cfg),
		resources.KruizeDeployment(cfg),
		resources.KruizeService(cfg),
	}
	for _, obj := range kruizeObjs {
		if err := r.apply(ctx, cfg, obj); err != nil {
			return Result{}, fmt.Errorf("kruize %s: %w", obj.GetName(), err)
		}
	}
	// Kruize ClusterRole/ClusterRoleBinding are cluster-scoped — no ownerRef possible.
	// TODO: add finalizer to clean these up when the CR is deleted (COST-7681).
	for _, obj := range []client.Object{resources.KruizeClusterRole(cfg), resources.KruizeClusterRoleBinding(cfg)} {
		if err := r.applyClusterScoped(ctx, obj); err != nil {
			return Result{}, fmt.Errorf("kruize rbac %s: %w", obj.GetName(), err)
		}
	}

	// Koku core + ROS shared config.
	objs := []client.Object{
		resources.CdappConfigMap(cfg),
		resources.ROSServiceAccount(cfg),
		resources.KokuAPIDeployment(cfg),
		resources.KokuAPIService(cfg),
		resources.MasuDeployment(cfg),
		resources.MasuService(cfg),
		resources.ListenerDeployment(cfg),
	}
	for _, obj := range objs {
		if err := r.apply(ctx, cfg, obj); err != nil {
			return Result{}, fmt.Errorf("core service %s: %w", obj.GetName(), err)
		}
	}

	// Gate on the API being available.
	ready, err := r.isDeploymentReady(ctx, cfg.Namespace, resources.NameKokuAPI(cfg))
	if err != nil {
		return Result{}, err
	}
	if !ready {
		r.setCondition(cfg, costv1alpha1.ConditionAvailable, metav1.ConditionFalse, "WaitingForAPI", "waiting for Koku API")
		return Result{RequeueAfter: requeueSlow}, nil
	}
	r.setCondition(cfg, costv1alpha1.ConditionAvailable, metav1.ConditionTrue, "KokuAvailable", "")
	return Result{}, nil
}

// -----------------------------------------------------------------------------
// Stage 5 — Workers and supporting services
// -----------------------------------------------------------------------------

func (r *CostManagementServiceConfigReconciler) reconcileWorkers(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig) (Result, error) {
	workers := resources.CeleryWorkerDeployments(cfg)
	objs := make([]client.Object, 0, 1+len(workers))

	// Celery beat + workers
	objs = append(objs, resources.CeleryBeatDeployment(cfg))
	for _, d := range workers {
		objs = append(objs, d)
	}

	// ROS components
	rosObjs := []client.Object{
		resources.ROSAPIDeployment(cfg),
		resources.ROSAPIService(cfg),
		resources.ROSProcessorDeployment(cfg),
		resources.ROSPollerDeployment(cfg),
		resources.ROSHousekeeperDeployment(cfg),
	}
	objs = append(objs, rosObjs...)

	// Kruize delete-partitions CronJob (if enabled)
	if costv1alpha1.BoolVal(cfg.Spec.Kruize.Partitions.DeleteEnabled, true) {
		objs = append(objs, resources.KruizeDeletePartitionsCronJob(cfg))
	}

	// ROS partition-cleaner CronJob (if enabled)
	if costv1alpha1.BoolVal(cfg.Spec.ROS.Housekeeper.PartitionCleaner.Enabled, true) {
		objs = append(objs, resources.ROSPartitionCleanerCronJob(cfg))
	}

	for _, obj := range objs {
		if err := r.apply(ctx, cfg, obj); err != nil {
			return Result{}, fmt.Errorf("worker %s: %w", obj.GetName(), err)
		}
	}

	return Result{}, nil
}

// -----------------------------------------------------------------------------
// Stage 6 — Edge: gateway, UI, routes
// -----------------------------------------------------------------------------

func (r *CostManagementServiceConfigReconciler) reconcileEdge(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig) (Result, error) {
	logger := log.FromContext(ctx)

	objs := []client.Object{
		resources.EnvoyConfigMap(cfg),
		resources.EnvoyService(cfg),
		resources.EnvoyDeployment(cfg),
	}
	for _, obj := range objs {
		if err := r.apply(ctx, cfg, obj); err != nil {
			return Result{}, fmt.Errorf("edge %s: %w", obj.GetName(), err)
		}
	}

	route := resources.GatewayAPIRoute(cfg)
	if route != nil {
		if err := r.apply(ctx, cfg, route); err != nil {
			return Result{}, fmt.Errorf("edge route %s: %w", route.GetName(), err)
		}
	} else {
		logger.Info("skipping API Route: cluster domain not resolved; gateway still deployed for in-cluster access")
	}

	ready, err := r.isDeploymentReady(ctx, cfg.Namespace, resources.NameEnvoy(cfg))
	if err != nil {
		return Result{}, err
	}
	if !ready {
		r.setCondition(cfg, costv1alpha1.ConditionAuthReady, metav1.ConditionFalse,
			"WaitingForGateway", "waiting for Envoy gateway Deployment")
		return Result{RequeueAfter: requeueSlow}, nil
	}

	if route == nil {
		r.setCondition(cfg, costv1alpha1.ConditionAuthReady, metav1.ConditionFalse,
			"ClusterDomainPending", "Envoy gateway ready; API Route deferred until cluster domain is available")
		return Result{RequeueAfter: requeueSlow}, nil
	}

	r.setCondition(cfg, costv1alpha1.ConditionAuthReady, metav1.ConditionTrue,
		"GatewayReady", "Envoy JWT gateway and API Route are ready")
	return Result{}, nil
}

// -----------------------------------------------------------------------------
// Apply / create helpers
// -----------------------------------------------------------------------------

// applyClusterScoped applies a cluster-scoped resource (no namespace, no ownerRef).
// These resources require finalizer-based cleanup — see COST-7681.
func (r *CostManagementServiceConfigReconciler) applyClusterScoped(ctx context.Context, obj client.Object) error {
	return r.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner))
}

// apply creates or updates obj using Server-Side Apply.
func (r *CostManagementServiceConfigReconciler) apply(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig, obj client.Object) error {
	obj.SetNamespace(cfg.Namespace)
	setOwnerRef(cfg, obj)
	return r.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner))
}

// applyStatefulSet applies a StatefulSet, handling the VolumeClaimTemplate
// immutability constraint by creating on first call and only patching spec
// (not VCT) on subsequent calls.
func (r *CostManagementServiceConfigReconciler) applyStatefulSet(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig, desired *appsv1.StatefulSet) error {
	existing := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Namespace: desired.Namespace, Name: desired.Name}, existing)
	if errors.IsNotFound(err) {
		setOwnerRef(cfg, desired)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	// Only update mutable fields (replicas, container image, resources).
	patch := existing.DeepCopy()
	patch.Spec.Replicas = desired.Spec.Replicas
	if len(patch.Spec.Template.Spec.Containers) > 0 && len(desired.Spec.Template.Spec.Containers) > 0 {
		patch.Spec.Template.Spec.Containers[0].Image = desired.Spec.Template.Spec.Containers[0].Image
		patch.Spec.Template.Spec.Containers[0].Resources = desired.Spec.Template.Spec.Containers[0].Resources
		patch.Spec.Template.Spec.Containers[0].Env = desired.Spec.Template.Spec.Containers[0].Env
	}
	return r.Update(ctx, patch)
}

// ensureSecret creates the secret only if it does not already exist.
// Existing secrets are never overwritten to preserve generated credentials.
func (r *CostManagementServiceConfigReconciler) ensureSecret(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig, secret *corev1.Secret) error {
	existing := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: secret.Namespace, Name: secret.Name}, existing)
	if errors.IsNotFound(err) {
		setOwnerRef(cfg, secret)
		return r.Create(ctx, secret)
	}
	return err
}

func setOwnerRef(owner *costv1alpha1.CostManagementServiceConfig, obj client.Object) {
	if obj.GetNamespace() == "" {
		return // cluster-scoped: owner refs don't apply, use finalizer instead
	}
	isController := true
	blockDeletion := true
	ref := metav1.OwnerReference{
		APIVersion:         costv1alpha1.GroupVersion.String(),
		Kind:               "CostManagementServiceConfig",
		Name:               owner.Name,
		UID:                owner.UID,
		Controller:         &isController,
		BlockOwnerDeletion: &blockDeletion,
	}
	refs := obj.GetOwnerReferences()
	for i, r := range refs {
		if r.Kind == ref.Kind && r.Name == ref.Name {
			refs[i] = ref
			obj.SetOwnerReferences(refs)
			return
		}
	}
	obj.SetOwnerReferences(append(refs, ref))
}

// -----------------------------------------------------------------------------
// Readiness helpers
// -----------------------------------------------------------------------------

func (r *CostManagementServiceConfigReconciler) isDeploymentReady(ctx context.Context, ns, name string) (bool, error) {
	d := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, d); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if d.Spec.Replicas == nil || *d.Spec.Replicas == 0 {
		return true, nil // 0 replicas = intentionally off
	}
	return d.Status.AvailableReplicas >= *d.Spec.Replicas, nil
}

func (r *CostManagementServiceConfigReconciler) isStatefulSetReady(ctx context.Context, ns, name string) (bool, error) {
	ss := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, ss); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if ss.Spec.Replicas == nil || *ss.Spec.Replicas == 0 {
		return true, nil
	}
	return ss.Status.ReadyReplicas >= *ss.Spec.Replicas, nil
}

func isJobComplete(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func isJobFailed(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// Status helpers
// -----------------------------------------------------------------------------

func (r *CostManagementServiceConfigReconciler) patchStatus(ctx context.Context, original, updated *costv1alpha1.CostManagementServiceConfig) error {
	return r.Status().Patch(ctx, updated, client.MergeFrom(original))
}

func (r *CostManagementServiceConfigReconciler) setCondition(cfg *costv1alpha1.CostManagementServiceConfig, condType string, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&cfg.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cfg.Generation,
	})
}

// -----------------------------------------------------------------------------
// Controller registration
// -----------------------------------------------------------------------------

func (r *CostManagementServiceConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&costv1alpha1.CostManagementServiceConfig{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.ServiceAccount{}).
		Complete(r)
}
