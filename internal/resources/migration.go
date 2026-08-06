package resources

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-server-operator/api/v1alpha1"
)

// MigrationJob builds the Koku Django migration Job.
// The operator deletes this Job before every upgrade and re-creates it so
// migrations always run with the new image before the API Deployment rolls out.
func MigrationJob(cfg *costv1alpha1.CostManagementServiceConfig, imageTag string) *batchv1.Job {
	backoff := int32(0)
	ttl := int32(3600) // clean up after 1 hour

	image := cfg.Spec.CostManagement.API.Image.Repository + ":" + cfg.Spec.CostManagement.API.Image.Tag
	if image == ":" {
		image = "quay.io/redhat-services-prod/cost-mgmt-dev-tenant/koku:latest"
	}

	env := KokuCommonEnv(cfg)
	env = append(env,
		EnvVal("MASU", "false"),
		EnvVal("PROMETHEUS_MULTIPROC_DIR", "/tmp"),
		EnvVal("KOKU_LOG_LEVEL", "INFO"),
		EnvVal("DJANGO_LOG_LEVEL", "INFO"),
	)

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      NameKokuMigration(cfg),
			Namespace: cfg.Namespace,
			Labels:    Labels(cfg, "cost-management-migration"),
			Annotations: map[string]string{
				// Track which image tag triggered this migration run so the
				// controller can detect when a new Job is needed on upgrade.
				"koku.costmanagement.io/image-tag": imageTag,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: Labels(cfg, "cost-management-migration"),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:           NameKokuServiceAccount(cfg),
					AutomountServiceAccountToken: boolPtr(false),
					RestartPolicy:                corev1.RestartPolicyNever,
					SecurityContext:              nonRootPodSC(),
					Containers: []corev1.Container{
						{
							Name:            "migrate",
							Image:           image,
							ImagePullPolicy: pullPolicy(cfg),
							Command:         []string{"bash", "-c", migrationScript()},
							Env:             env,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("250m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
							VolumeMounts: KokuVolumeMounts(cfg),
							// readOnlyRootFilesystem omitted: Django instantiates all
							// configured log handler objects (including file handlers)
							// before our DJANGO_LOG_HANDLERS override takes effect.
							SecurityContext: migrationContainerSC(),
						},
					},
					InitContainers: []corev1.Container{
						CACombineInitContainer(cfg),
					},
					Volumes: KokuVolumes(cfg),
				},
			},
		},
	}
}

func migrationScript() string {
	return `set -e
echo "=== Koku Django Migrations ==="
DB_HOST="${DATABASE_SERVICE_HOST}"
DB_PORT="${DATABASE_SERVICE_PORT:-5432}"
ELAPSED=0
echo "Waiting for database at ${DB_HOST}:${DB_PORT}..."
while true; do
  if [ $ELAPSED -ge 600 ]; then echo "ERROR: DB not ready after 600s"; exit 1; fi
  if timeout 5 bash -c "cat < /dev/null > /dev/tcp/${DB_HOST}/${DB_PORT}" 2>/dev/null; then break; fi
  echo "DB not reachable (${ELAPSED}s elapsed), waiting..."
  sleep 5; ELAPSED=$((ELAPSED + 5))
done
echo "DB ready after ${ELAPSED}s"
mkdir -p /tmp/prometheus
cd /opt/koku/koku
python manage.py migrate --noinput
echo "=== Migrations completed ==="`
}

func boolPtr(b bool) *bool { return &b }
