package resources

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-server-operator/api/v1alpha1"
)

const (
	rbacDBName = "costonprem_rbac"

	// MigrationBackoffLimit is the number of Kubernetes retries per Job,
	// matching the COST-7685 specification.
	MigrationBackoffLimit = int32(3)
	// MigrationDeadlineSeconds is the hard timeout per migration Job.
	MigrationDeadlineSeconds = int64(600)
)

// NameROSMigration returns the ROS migration Job name.
func NameROSMigration(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-ros-migrate"
}

// NameRBACMigration returns the RBAC migration Job name.
func NameRBACMigration(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-rbac-migrate"
}

// -----------------------------------------------------------------------------
// Koku migration
// -----------------------------------------------------------------------------

// MigrationJob builds the Koku Django migration Job.
func MigrationJob(cfg *costv1alpha1.CostManagementServiceConfig, imageTag string) *batchv1.Job {
	backoff := MigrationBackoffLimit
	deadline := MigrationDeadlineSeconds
	ttl := int32(3600)

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

	return migrationJob(cfg, NameKokuMigration(cfg), image, imageTag,
		"cost-management-migration", migrationScript(), env,
		KokuVolumeMounts(cfg), KokuVolumes(cfg),
		[]corev1.Container{CACombineInitContainer(cfg)},
		backoff, deadline, ttl,
	)
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
echo "Migrations completed successfully"
echo "=== Migrations completed ==="`
}

// -----------------------------------------------------------------------------
// ROS migration
// -----------------------------------------------------------------------------

// ROSMigrationJob builds the ROS schema migration Job.
func ROSMigrationJob(cfg *costv1alpha1.CostManagementServiceConfig, imageTag string) *batchv1.Job {
	backoff := MigrationBackoffLimit
	deadline := MigrationDeadlineSeconds
	ttl := int32(3600)

	image := cfg.Spec.ROS.Image.Repository + ":" + cfg.Spec.ROS.Image.Tag

	dbSecret := NameDBCredentials(cfg)
	host := DatabaseHost(cfg)
	port := cfg.Spec.Database.Port
	if port == 0 {
		port = 5432
	}

	env := []corev1.EnvVar{
		EnvVal("CLOWDER_ENABLED", "false"),
		EnvVal("DB_HOST", host),
		EnvVal("DB_PORT", int32String(port)),
		EnvVal("DB_NAME", rosDBName),
		EnvFromSecret("DB_USER", dbSecret, "ros-user"),
		EnvFromSecret("DB_PASSWORD", dbSecret, "ros-password"),
		EnvVal("LOG_LEVEL", "INFO"),
	}

	script := rosMigrationScript(host, int32String(port))

	vols := []corev1.Volume{{
		Name:         "tmp",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}}
	mounts := []corev1.VolumeMount{{Name: "tmp", MountPath: "/tmp"}}

	return migrationJob(cfg, NameROSMigration(cfg), image, imageTag,
		"ros-migration", script, env, mounts, vols, nil,
		backoff, deadline, ttl,
	)
}

func rosMigrationScript(host, port string) string {
	return `set -e
echo "=== ROS Database Migrations ==="
DB_HOST="${DB_HOST:-` + host + `}"
DB_PORT="${DB_PORT:-` + port + `}"
ELAPSED=0
echo "Waiting for database at ${DB_HOST}:${DB_PORT}..."
while true; do
  if [ $ELAPSED -ge 600 ]; then echo "ERROR: DB not ready after 600s"; exit 1; fi
  if timeout 5 bash -c "cat < /dev/null > /dev/tcp/${DB_HOST}/${DB_PORT}" 2>/dev/null; then
    sleep 5; break
  fi
  echo "DB not reachable (${ELAPSED}s elapsed), waiting..."
  sleep 5; ELAPSED=$((ELAPSED + 5))
done
echo "DB ready after ${ELAPSED}s"
./rosocp db migrate up
echo "=== ROS migrations completed ==="`
}

// -----------------------------------------------------------------------------
// RBAC migration + seeding
// -----------------------------------------------------------------------------

// RBACMigrationJob builds the RBAC schema migration + built-in seeding Job.
// This combines `manage.py migrate`, `manage.py seeds`, and public tenant
// bootstrapping into a single Job — the same pattern as the Helm chart.
func RBACMigrationJob(cfg *costv1alpha1.CostManagementServiceConfig, imageTag string) *batchv1.Job {
	backoff := MigrationBackoffLimit
	deadline := MigrationDeadlineSeconds
	ttl := int32(3600)

	image := cfg.Spec.RBAC.Image.Repository + ":" + cfg.Spec.RBAC.Image.Tag

	env := rbacMigrationEnv(cfg)
	script := rbacMigrationScript(DatabaseHost(cfg), int32String(cfg.Spec.Database.Port))

	vols := []corev1.Volume{{
		Name:         "tmp",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}}
	mounts := []corev1.VolumeMount{{Name: "tmp", MountPath: "/tmp"}}

	return migrationJob(cfg, NameRBACMigration(cfg), image, imageTag,
		"rbac-migration", script, env, mounts, vols, nil,
		backoff, deadline, ttl,
	)
}

func rbacMigrationEnv(cfg *costv1alpha1.CostManagementServiceConfig) []corev1.EnvVar {
	// Same on-prem env as API/worker so seeds (V2 UUIDs, BOP bypass) succeed.
	return rbacEnv(cfg)
}

func rbacMigrationScript(host, port string) string {
	return `set -e
echo "=== RBAC Database Migrations ==="
DB_HOST="${DATABASE_HOST:-` + host + `}"
DB_PORT="${DATABASE_PORT:-` + port + `}"
ELAPSED=0
echo "Waiting for database at ${DB_HOST}:${DB_PORT}..."
while true; do
  if [ $ELAPSED -ge 300 ]; then echo "ERROR: DB not ready after 300s"; exit 1; fi
  if timeout 5 bash -c "cat < /dev/null > /dev/tcp/${DB_HOST}/${DB_PORT}" 2>/dev/null; then break; fi
  echo "DB not reachable (${ELAPSED}s elapsed), waiting..."
  sleep 5; ELAPSED=$((ELAPSED + 5))
done
echo "DB ready after ${ELAPSED}s"
cd /opt/rbac/rbac
python manage.py migrate --noinput
echo "=== Schema migrations complete ==="
python manage.py seeds --skip-notifications
echo "=== Built-in role seeding complete ==="
python manage.py bootstrap_tenants --all -v 2 || echo "WARNING: bootstrap_tenants non-fatal, continuing"
echo "=== RBAC migrations and seeding completed ==="`
}

// -----------------------------------------------------------------------------
// Shared job builder
// -----------------------------------------------------------------------------

func migrationJob(
	cfg *costv1alpha1.CostManagementServiceConfig,
	name, image, imageTag, component, script string,
	env []corev1.EnvVar,
	mounts []corev1.VolumeMount,
	vols []corev1.Volume,
	initContainers []corev1.Container,
	backoff int32, deadline int64, ttlSecs int32,
) *batchv1.Job {
	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cfg.Namespace,
			Labels:    Labels(cfg, component),
			Annotations: map[string]string{
				"koku.costmanagement.io/image-tag": imageTag,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			ActiveDeadlineSeconds:   &deadline,
			TTLSecondsAfterFinished: &ttlSecs,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: Labels(cfg, component)},
				Spec: corev1.PodSpec{
					ServiceAccountName:           NameKokuServiceAccount(cfg),
					AutomountServiceAccountToken: boolPtr(false),
					RestartPolicy:                corev1.RestartPolicyOnFailure,
					SecurityContext:              nonRootPodSC(),
					InitContainers:               initContainers,
					Containers: []corev1.Container{{
						Name:            "migrate",
						Image:           image,
						ImagePullPolicy: pullPolicy(cfg),
						Command:         []string{"bash", "-c", script},
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
						VolumeMounts:    mounts,
						SecurityContext: migrationContainerSC(),
					}},
					Volumes: vols,
				},
			},
		},
	}
}

func boolPtr(b bool) *bool { return &b }
