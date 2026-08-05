package resources

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	costv1alpha1 "github.com/project-koku/koku-server-operator/api/v1alpha1"
)

// KokuCommonEnv builds the environment variables shared by all Koku
// containers (API, Masu, Listener, Celery workers, migration Job).
// Mirrors the cost-onprem.koku.commonEnv Helm helper.
func KokuCommonEnv(cfg *costv1alpha1.CostManagementServiceConfig) []corev1.EnvVar {
	dbSecret := NameDBCredentials(cfg)
	storageSecret := NameStorageSecret(cfg)
	djangoSecret := NameDjangoSecret(cfg)

	cachePort := fmt.Sprintf("%d", cfg.Spec.Cache.Port)
	if cfg.Spec.Cache.Port == 0 {
		cachePort = "6379"
	}
	dbPort := fmt.Sprintf("%d", cfg.Spec.Database.Port)
	if cfg.Spec.Database.Port == 0 {
		dbPort = "5432"
	}

	env := []corev1.EnvVar{
		EnvVal("ONPREM", "True"),
		EnvVal("DATABASE_SERVICE_NAME", "database"),
		EnvVal("DATABASE_ENGINE", "postgresql"),
		EnvVal("DATABASE_SERVICE_HOST", DatabaseHost(cfg)),
		EnvVal("DATABASE_SERVICE_PORT", dbPort),
		EnvVal("DATABASE_NAME", "costonprem_koku"),
		EnvFromSecret("DATABASE_USER", dbSecret, "koku-user"),
		EnvFromSecret("DATABASE_PASSWORD", dbSecret, "koku-password"),
		EnvVal("REDIS_HOST", CacheHost(cfg)),
		EnvVal("REDIS_PORT", cachePort),
		EnvVal("INSIGHTS_KAFKA_HOST", KafkaHost(cfg)),
		EnvVal("INSIGHTS_KAFKA_PORT", KafkaPort(cfg)),
		EnvVal("S3_ENDPOINT", S3Endpoint(cfg)),
		EnvVal("REQUESTED_BUCKET", cfg.Spec.CostManagement.Storage.BucketName),
		EnvVal("REQUESTED_ROS_BUCKET", cfg.Spec.CostManagement.Storage.ROSBucketName),
		EnvVal("AWS_CA_BUNDLE", "/etc/pki/ca-trust/combined/ca-bundle.crt"),
		EnvVal("REQUESTS_CA_BUNDLE", "/etc/pki/ca-trust/combined/ca-bundle.crt"),
		EnvFromSecretOptional("AWS_ACCESS_KEY_ID", storageSecret, "access-key"),
		EnvFromSecretOptional("AWS_SECRET_ACCESS_KEY", storageSecret, "secret-key"),
		EnvVal("S3_REGION", cfg.Spec.ObjectStorage.S3.Region),
		EnvVal("AWS_CONFIG_FILE", "/etc/aws/config"),
		EnvFromSecret("DJANGO_SECRET_KEY", djangoSecret, "secret-key"),
		EnvVal("SCHEDULE_REPORT_CHECKS", boolStr(cfg.Spec.CostManagement.ScheduleReportChecks)),
		EnvVal("REPORT_DOWNLOAD_SCHEDULE", cfg.Spec.CostManagement.ReportDownloadSchedule),
		EnvVal("RBAC_SERVICE_HOST", NameRBACAPI(cfg)),
		EnvVal("RBAC_SERVICE_PORT", "8080"),
		EnvVal("RBAC_SERVICE_PATH", "/api/rbac/v1/access/"),
		EnvVal("RBAC_SERVICE_PROTOCOL", "http"),
	}

	// Celery result expiry (default 28800 = 8 hours)
	env = append(env, EnvVal("CELERY_RESULT_EXPIRES", "28800"))

	// Default to console-only logging. The koku settings.py configures a file
	// handler; setting this env var overrides which handlers loggers use so
	// Django doesn't try to write logs to the (read-only) container filesystem.
	env = append(env, EnvVal("DJANGO_LOG_HANDLERS", "console"))

	// Optional: Valkey auth
	if cfg.Spec.Cache.Auth.Enabled && cfg.Spec.Cache.Auth.SecretName != "" {
		env = append(env,
			EnvFromSecretOptional("REDIS_USERNAME", cfg.Spec.Cache.Auth.SecretName, "redis-username"),
			EnvFromSecret("REDIS_PASSWORD", cfg.Spec.Cache.Auth.SecretName, "redis-password"),
		)
	}

	// Optional: Valkey TLS
	if cfg.Spec.Cache.TLS.Enabled {
		env = append(env, EnvVal("REDIS_SSL", "True"))
		if cfg.Spec.Cache.TLS.CACertSecretName != "" {
			env = append(env, EnvVal("REDIS_SSL_CA_CERTS", "/etc/redis-tls/ca.crt"))
		}
	}

	// Optional: currency URL
	if cfg.Spec.CostManagement.API.Env["CURRENCY_URL"] != "" {
		env = append(env, EnvVal("CURRENCY_URL", cfg.Spec.CostManagement.API.Env["CURRENCY_URL"]))
	}

	return env
}

func boolStr(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

// MergeEnv appends user-provided env overrides after the base set,
// letting user-specified values take precedence for duplicate keys.
func MergeEnv(base []corev1.EnvVar, overrides map[string]string) []corev1.EnvVar {
	for k, v := range overrides {
		base = append(base, EnvVal(k, v))
	}
	return base
}
