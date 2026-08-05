package resources

import (
	"strconv"

	costv1alpha1 "github.com/project-koku/koku-server-operator/api/v1alpha1"
)

// Names derives resource names from the CR name so they are deterministic
// and consistent across reconcile loops.

func NameDatabase(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-database"
}

func NameValkey(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-valkey"
}

func NameValkeyPVC(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-valkey-data"
}

func NameDBCredentials(cfg *costv1alpha1.CostManagementServiceConfig) string {
	if cfg.Spec.Database.SecretName != "" {
		return cfg.Spec.Database.SecretName
	}
	return cfg.Name + "-db-credentials"
}

func NameDBInitConfigMap(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-db-init"
}

func NameDjangoSecret(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-django-secret"
}

func NameStorageSecret(cfg *costv1alpha1.CostManagementServiceConfig) string {
	if cfg.Spec.ObjectStorage.SecretName != "" {
		return cfg.Spec.ObjectStorage.SecretName
	}
	return cfg.Name + "-storage-credentials"
}

func NameAWSConfigMap(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-aws-config"
}

func NameCACombineConfigMap(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-ca-combine"
}

func NameServiceCAConfigMap(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-service-ca"
}

func NameKokuMigration(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-koku-migrate"
}

func NameKokuAPI(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-koku-api"
}

func NameKokuServiceAccount(cfg *costv1alpha1.CostManagementServiceConfig) string {
	if cfg.Spec.CostManagement.ServiceAccount.Name != "" {
		return cfg.Spec.CostManagement.ServiceAccount.Name
	}
	return cfg.Name + "-koku"
}

func NameMasu(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-koku-masu"
}

func NameListener(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-koku-listener"
}

func NameCeleryBeat(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-celery-beat"
}

func NameCeleryWorker(cfg *costv1alpha1.CostManagementServiceConfig, queue string) string {
	return cfg.Name + "-celery-worker-" + queue
}

// DatabaseHost returns the hostname of the database that all services should connect to.
func DatabaseHost(cfg *costv1alpha1.CostManagementServiceConfig) string {
	if cfg.Spec.Database.Deploy {
		return NameDatabase(cfg)
	}
	return cfg.Spec.Database.Host
}

// CacheHost returns the hostname of the Valkey/Redis instance.
func CacheHost(cfg *costv1alpha1.CostManagementServiceConfig) string {
	if cfg.Spec.Cache.Deploy {
		return NameValkey(cfg)
	}
	return cfg.Spec.Cache.Host
}

// KafkaHost parses the bootstrap servers string and returns just the hostname.
func KafkaHost(cfg *costv1alpha1.CostManagementServiceConfig) string {
	bs := cfg.Spec.Kafka.BootstrapServers
	for i := len(bs) - 1; i >= 0; i-- {
		if bs[i] == ':' {
			return bs[:i]
		}
	}
	return bs
}

// KafkaPort parses the bootstrap servers string and returns just the port.
func KafkaPort(cfg *costv1alpha1.CostManagementServiceConfig) string {
	bs := cfg.Spec.Kafka.BootstrapServers
	for i := len(bs) - 1; i >= 0; i-- {
		if bs[i] == ':' {
			return bs[i+1:]
		}
	}
	return "9092"
}

// S3Endpoint returns the S3 endpoint URL including protocol.
func S3Endpoint(cfg *costv1alpha1.CostManagementServiceConfig) string {
	s := cfg.Spec.ObjectStorage
	scheme := "http"
	port := s.Port
	if s.UseSSL {
		scheme = "https"
		if port == 0 {
			port = 443
		}
	} else if port == 0 {
		port = 80
	}
	return scheme + "://" + s.Endpoint + ":" + int32String(port)
}

func int32String(n int32) string {
	return strconv.Itoa(int(n))
}

func NameRBACAPI(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-rbac-api"
}

func NameRBACWorker(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-rbac-worker"
}

func NameROSAPI(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-ros-api"
}

func NameROSProcessor(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-ros-processor"
}

func NameROSPoller(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-ros-recommendation-poller"
}

func NameROSHousekeeper(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-ros-housekeeper"
}

func NameKruize(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-kruize"
}

func NameEnvoy(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-gateway"
}

func NameUI(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-ui"
}

func NameIngress(cfg *costv1alpha1.CostManagementServiceConfig) string {
	return cfg.Name + "-ingress"
}
