package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// -----------------------------------------------------------------------------
// Shared primitives
// -----------------------------------------------------------------------------

type ImageSpec struct {
	Repository string            `json:"repository"`
	Tag        string            `json:"tag"`
	// +kubebuilder:default:=IfNotPresent
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
}

type ServiceAccountSpec struct {
	// +kubebuilder:default:=true
	Create bool   `json:"create,omitempty"`
	Name   string `json:"name,omitempty"`
}

// SecretKeyRef points to a key inside a named Secret.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// -----------------------------------------------------------------------------
// GlobalConfig
// -----------------------------------------------------------------------------

type GlobalConfig struct {
	// +kubebuilder:default:=IfNotPresent
	PullPolicy      corev1.PullPolicy            `json:"pullPolicy,omitempty"`
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Cluster base domain used for Route hostname generation.
	// +kubebuilder:default:="apps.cluster.local"
	ClusterDomain string `json:"clusterDomain,omitempty"`
	// StorageClass for all PVCs. If empty, the cluster default is used.
	StorageClass string `json:"storageClass,omitempty"`
}

// -----------------------------------------------------------------------------
// DatabaseConfig
// -----------------------------------------------------------------------------

type DatabaseConfig struct {
	// Deploy the bundled PostgreSQL StatefulSet.
	// Set false to use an external database (requires Host to be set).
	// +kubebuilder:default:=true
	Deploy bool      `json:"deploy,omitempty"`
	Image  ImageSpec `json:"image,omitempty"`
	Storage DatabaseStorageSpec `json:"storage,omitempty"`

	// Host for an external PostgreSQL instance (only used when Deploy is false).
	Host string `json:"host,omitempty"`
	// +kubebuilder:default:=5432
	Port int32 `json:"port,omitempty"`
	// +kubebuilder:default:=disable
	SSLMode string `json:"sslMode,omitempty"`

	// Name of an existing Secret containing DB credentials.
	// The secret must have keys: postgres-user, postgres-password,
	// koku-user, koku-password, ros-user, ros-password,
	// kruize-user, kruize-password, rbac-user, rbac-password.
	// When empty the operator generates random credentials and stores them
	// in a Secret named <cr-name>-db-credentials.
	SecretName string `json:"secretName,omitempty"`

	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

type DatabaseStorageSpec struct {
	// +kubebuilder:default:="30Gi"
	Size resource.Quantity `json:"size,omitempty"`
}

// -----------------------------------------------------------------------------
// CacheConfig (Valkey / Redis)
// -----------------------------------------------------------------------------

type CacheConfig struct {
	// Deploy the bundled Valkey instance.
	// Set false to use an external Redis/Valkey endpoint.
	// +kubebuilder:default:=true
	Deploy bool      `json:"deploy,omitempty"`
	Image  ImageSpec `json:"image,omitempty"`

	// Host for an external cache (only used when Deploy is false).
	Host string `json:"host,omitempty"`
	// +kubebuilder:default:=6379
	Port int32 `json:"port,omitempty"`

	Auth        CacheAuthSpec        `json:"auth,omitempty"`
	TLS         CacheTLSSpec         `json:"tls,omitempty"`
	Persistence CachePersistenceSpec `json:"persistence,omitempty"`
	Resources   corev1.ResourceRequirements `json:"resources,omitempty"`
}

type CacheAuthSpec struct {
	Enabled bool   `json:"enabled,omitempty"`
	// Name of a Secret with key redis-password (and optionally redis-username).
	SecretName string `json:"secretName,omitempty"`
}

type CacheTLSSpec struct {
	Enabled bool `json:"enabled,omitempty"`
	// Name of a Secret with key ca.crt for certificate verification.
	CACertSecretName string `json:"caCertSecretName,omitempty"`
}

type CachePersistenceSpec struct {
	// +kubebuilder:default:="5Gi"
	Size resource.Quantity `json:"size,omitempty"`
}

// -----------------------------------------------------------------------------
// KafkaConfig — connection only; Kafka is managed by AMQ Streams externally
// -----------------------------------------------------------------------------

type KafkaConfig struct {
	// Bootstrap servers for the Kafka cluster.
	// +kubebuilder:default:="cost-onprem-kafka-kafka-bootstrap.kafka.svc.cluster.local:9092"
	BootstrapServers string `json:"bootstrapServers"`
	// +kubebuilder:default:=PLAINTEXT
	// +kubebuilder:validation:Enum=PLAINTEXT;SSL;SASL_PLAINTEXT;SASL_SSL
	SecurityProtocol string        `json:"securityProtocol,omitempty"`
	SASL             KafkaSASLSpec `json:"sasl,omitempty"`
	TLS              KafkaTLSSpec  `json:"tls,omitempty"`
}

type KafkaSASLSpec struct {
	// +kubebuilder:validation:Enum=PLAIN;SCRAM-SHA-256;SCRAM-SHA-512;""
	Mechanism      string `json:"mechanism,omitempty"`
	// Name of a Secret with keys: username, password.
	ExistingSecret string `json:"existingSecret,omitempty"`
}

type KafkaTLSSpec struct {
	Enabled bool `json:"enabled,omitempty"`
	// Name of a Secret with key ca.crt.
	CACertSecret string `json:"caCertSecret,omitempty"`
}

// -----------------------------------------------------------------------------
// ObjectStorageConfig (S3-compatible)
// -----------------------------------------------------------------------------

type ObjectStorageConfig struct {
	// S3 endpoint hostname (without protocol or port).
	// +kubebuilder:default:="s3.openshift-storage.svc.cluster.local"
	Endpoint string `json:"endpoint,omitempty"`
	// +kubebuilder:default:=443
	Port int32 `json:"port,omitempty"`
	// +kubebuilder:default:=true
	UseSSL bool `json:"useSSL,omitempty"`
	// Name of an existing Secret with keys: access-key, secret-key.
	// When empty the operator creates the secret from ODF/NooBaa.
	SecretName string    `json:"secretName,omitempty"`
	S3         S3Options `json:"s3,omitempty"`
}

type S3Options struct {
	// +kubebuilder:default:=onprem
	Region string `json:"region,omitempty"`
	// +kubebuilder:default:=path
	// +kubebuilder:validation:Enum=path;auto;virtual
	AddressingStyle string `json:"addressingStyle,omitempty"`
}

// -----------------------------------------------------------------------------
// AuthConfig (JWT via Envoy + Keycloak/RHBK)
// -----------------------------------------------------------------------------

type AuthConfig struct {
	Envoy      EnvoySpec    `json:"envoy,omitempty"`
	Keycloak   KeycloakSpec `json:"keycloak,omitempty"`
	RealmUsers []RealmUser  `json:"realmUsers,omitempty"`
}

type EnvoySpec struct {
	Image     ImageSpec                   `json:"image,omitempty"`
	// +kubebuilder:default:=2
	Replicas  int32                       `json:"replicas,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

type KeycloakSpec struct {
	// Full URL of the Keycloak instance. Auto-detected when empty.
	URL string `json:"url,omitempty"`
	// Keycloak namespace. Defaults to "keycloak".
	Namespace string `json:"namespace,omitempty"`
	// +kubebuilder:default:=kubernetes
	Realm string `json:"realm,omitempty"`
	// JWT audiences accepted by the gateway.
	// +kubebuilder:default:={"cost-management-operator","cost-management-ui"}
	Audiences []string `json:"audiences,omitempty"`
	TLS       KeycloakTLSSpec `json:"tls,omitempty"`
}

type KeycloakTLSSpec struct {
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

type RealmUser struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	Email         string `json:"email,omitempty"`
	FirstName     string `json:"firstName,omitempty"`
	LastName      string `json:"lastName,omitempty"`
	OrgID         string `json:"orgId,omitempty"`
	AccountNumber string `json:"accountNumber,omitempty"`
	OrgAdmin      bool   `json:"orgAdmin,omitempty"`
}

// -----------------------------------------------------------------------------
// RBACConfig (insights-rbac)
// -----------------------------------------------------------------------------

type RBACConfig struct {
	Image          ImageSpec                   `json:"image,omitempty"`
	API            RBACComponentSpec           `json:"api,omitempty"`
	Worker         RBACComponentSpec           `json:"worker,omitempty"`
	BootstrapAdmin BootstrapAdminSpec          `json:"bootstrapAdmin,omitempty"`
	KeycloakSync   KeycloakSyncSpec            `json:"keycloakSync,omitempty"`
}

type RBACComponentSpec struct {
	// +kubebuilder:default:=1
	Replicas  int32                       `json:"replicas,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

type BootstrapAdminSpec struct {
	Enabled bool `json:"enabled,omitempty"`
}

type KeycloakSyncSpec struct {
	Enabled bool   `json:"enabled,omitempty"`
	// +kubebuilder:default:="*/15 * * * *"
	Schedule         string       `json:"schedule,omitempty"`
	OrgGroupPrefix   string       `json:"orgGroupPrefix,omitempty"`
	OrgAdminSubgroup string       `json:"orgAdminSubgroup,omitempty"`
	PruneOrphans     bool         `json:"pruneOrphans,omitempty"`
	ClientID         string       `json:"clientId,omitempty"`
	ClientSecretRef  SecretKeyRef `json:"clientSecretRef,omitempty"`
	Resources        corev1.ResourceRequirements `json:"resources,omitempty"`
}

// -----------------------------------------------------------------------------
// IngressConfig (insights-ingress-go upload handler)
// -----------------------------------------------------------------------------

type IngressConfig struct {
	Image   ImageSpec `json:"image,omitempty"`
	// Maximum upload size in bytes.
	// +kubebuilder:default:=104857600
	MaxUploadSize int64 `json:"maxUploadSize,omitempty"`
	// Comma-separated list of valid upload content types.
	// +kubebuilder:default:="hccm"
	ValidTypes string `json:"validTypes,omitempty"`
	// Staging bucket name for uploads.
	// +kubebuilder:default:="insights-upload-perma"
	StagingBucket string `json:"stagingBucket,omitempty"`
	Resources     corev1.ResourceRequirements `json:"resources,omitempty"`
}

// -----------------------------------------------------------------------------
// KruizeConfig (resource optimization engine, used by ROS)
// -----------------------------------------------------------------------------

type KruizeConfig struct {
	// +kubebuilder:default:=1
	Replicas   int32                       `json:"replicas,omitempty"`
	Image      ImageSpec                   `json:"image,omitempty"`
	Resources  corev1.ResourceRequirements `json:"resources,omitempty"`
	Partitions KruizePartitionsSpec        `json:"partitions,omitempty"`
}

type KruizePartitionsSpec struct {
	// +kubebuilder:default:=true
	CreateEnabled bool `json:"createEnabled,omitempty"`
	// +kubebuilder:default:=true
	DeleteEnabled bool `json:"deleteEnabled,omitempty"`
	// +kubebuilder:default:="0 0 * * *"
	DeleteSchedule            string `json:"deleteSchedule,omitempty"`
	// +kubebuilder:default:="16"
	DeletePartitionsThreshold string `json:"deletePartitionsThreshold,omitempty"`
	Resources                 corev1.ResourceRequirements `json:"resources,omitempty"`
}

// -----------------------------------------------------------------------------
// ROSConfig (Resource Optimization Service)
// -----------------------------------------------------------------------------

type ROSConfig struct {
	Image          ImageSpec          `json:"image,omitempty"`
	ServiceAccount ServiceAccountSpec `json:"serviceAccount,omitempty"`
	API            ROSAPISpec         `json:"api,omitempty"`
	Processor      ROSProcessorSpec   `json:"processor,omitempty"`
	RecommendationPoller ROSPollerSpec `json:"recommendationPoller,omitempty"`
	Housekeeper    ROSHousekeeperSpec `json:"housekeeper,omitempty"`
}

type ROSAPISpec struct {
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// +kubebuilder:default:=INFO
	LogLevel string `json:"logLevel,omitempty"`
}

type ROSProcessorSpec struct {
	// +kubebuilder:default:=1
	Replicas  int32                       `json:"replicas,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// +kubebuilder:default:=INFO
	LogLevel string `json:"logLevel,omitempty"`
}

type ROSPollerSpec struct {
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// +kubebuilder:default:=INFO
	LogLevel string `json:"logLevel,omitempty"`
}

type ROSHousekeeperSpec struct {
	Resources       corev1.ResourceRequirements `json:"resources,omitempty"`
	PartitionCleaner ROSPartitionCleanerSpec    `json:"partitionCleaner,omitempty"`
}

type ROSPartitionCleanerSpec struct {
	// +kubebuilder:default:=true
	Enabled bool `json:"enabled,omitempty"`
	// +kubebuilder:default:="0 0 */15 * *"
	Schedule  string                      `json:"schedule,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// -----------------------------------------------------------------------------
// CostManagementConfig (Koku API, Masu, Celery, Listener)
// -----------------------------------------------------------------------------

type CostManagementConfig struct {
	// +kubebuilder:default:=true
	ScheduleReportChecks bool `json:"scheduleReportChecks,omitempty"`
	// Cron expression for report download checks.
	// +kubebuilder:default:="*/5 * * * *"
	ReportDownloadSchedule string `json:"reportDownloadSchedule,omitempty"`

	Storage        CostManagementStorageSpec `json:"storage,omitempty"`
	API            KokuAPISpec               `json:"api,omitempty"`
	Masu           MasuSpec                  `json:"masu,omitempty"`
	Listener       ListenerSpec              `json:"listener,omitempty"`
	Celery         CelerySpec                `json:"celery,omitempty"`
	ServiceAccount ServiceAccountSpec        `json:"serviceAccount,omitempty"`
}

type CostManagementStorageSpec struct {
	// +kubebuilder:default:="koku-bucket"
	BucketName string `json:"bucketName,omitempty"`
	// +kubebuilder:default:="ros-data"
	ROSBucketName string `json:"rosBucketName,omitempty"`
}

type KokuAPISpec struct {
	// +kubebuilder:default:=true
	Enabled  bool      `json:"enabled,omitempty"`
	Image    ImageSpec `json:"image,omitempty"`
	// +kubebuilder:default:=1
	Replicas  int32                       `json:"replicas,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// Environment variable overrides injected into the container.
	// These are merged over the operator-managed defaults.
	Env map[string]string `json:"env,omitempty"`
}

type MasuSpec struct {
	// +kubebuilder:default:=true
	Enabled  bool      `json:"enabled,omitempty"`
	Image    ImageSpec `json:"image,omitempty"`
	// +kubebuilder:default:=1
	Replicas  int32                       `json:"replicas,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	Env       map[string]string           `json:"env,omitempty"`
}

type ListenerSpec struct {
	// +kubebuilder:default:=true
	Enabled  bool `json:"enabled,omitempty"`
	// +kubebuilder:default:=2
	Replicas  int32                       `json:"replicas,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	Env       map[string]string           `json:"env,omitempty"`
}

type CelerySpec struct {
	Workers CeleryWorkersSpec `json:"workers,omitempty"`
}

type CeleryWorkersSpec struct {
	Default         CeleryWorkerSpec `json:"default,omitempty"`
	Priority        CeleryWorkerSpec `json:"priority,omitempty"`
	Summary         CeleryWorkerSpec `json:"summary,omitempty"`
	OCP             CeleryWorkerSpec `json:"ocp,omitempty"`
	CostModel       CeleryWorkerSpec `json:"costModel,omitempty"`
	// Cloud-provider workers — typically 0 replicas for OCP-only deployments.
	Refresh         CeleryWorkerSpec `json:"refresh,omitempty"`
	HCS             CeleryWorkerSpec `json:"hcs,omitempty"`
	Download        CeleryWorkerSpec `json:"download,omitempty"`
	SubsExtraction  CeleryWorkerSpec `json:"subsExtraction,omitempty"`
	SubsTransmission CeleryWorkerSpec `json:"subsTransmission,omitempty"`
}

type CeleryWorkerSpec struct {
	// +kubebuilder:default:=1
	Replicas    int32                       `json:"replicas,omitempty"`
	// +kubebuilder:default:=5
	Concurrency int32                       `json:"concurrency,omitempty"`
	Resources   corev1.ResourceRequirements `json:"resources,omitempty"`
}

// -----------------------------------------------------------------------------
// UIConfig
// -----------------------------------------------------------------------------

type UIConfig struct {
	// +kubebuilder:default:=1
	ReplicaCount int32           `json:"replicaCount,omitempty"`
	OAuthProxy   OAuthProxySpec  `json:"oauthProxy,omitempty"`
	App          UIAppSpec       `json:"app,omitempty"`
}

type OAuthProxySpec struct {
	Image     ImageSpec                   `json:"image,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// Cookie session expiration (e.g. "720h").
	// +kubebuilder:default:="720h"
	CookieExpire string `json:"cookieExpire,omitempty"`
}

type UIAppSpec struct {
	Image     ImageSpec                   `json:"image,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// -----------------------------------------------------------------------------
// GatewayRouteConfig (OpenShift Route for the Envoy gateway)
// -----------------------------------------------------------------------------

type GatewayRouteConfig struct {
	Annotations map[string]string    `json:"annotations,omitempty"`
	Hosts       []RouteHostSpec      `json:"hosts,omitempty"`
	TLS         RouteTLSSpec         `json:"tls,omitempty"`
}

type RouteHostSpec struct {
	// Empty host uses the cluster's default ingress domain.
	Host  string          `json:"host,omitempty"`
	Paths []RoutePathSpec `json:"paths,omitempty"`
}

type RoutePathSpec struct {
	// +kubebuilder:default:="/"
	Path     string `json:"path,omitempty"`
	// +kubebuilder:default:=Prefix
	PathType string `json:"pathType,omitempty"`
}

type RouteTLSSpec struct {
	// +kubebuilder:default:=edge
	// +kubebuilder:validation:Enum=edge;passthrough;reencrypt
	Termination string `json:"termination,omitempty"`
	// +kubebuilder:default:=Redirect
	// +kubebuilder:validation:Enum=Allow;Redirect;None
	InsecureEdgeTerminationPolicy string `json:"insecureEdgeTerminationPolicy,omitempty"`
}

// -----------------------------------------------------------------------------
// MonitoringConfig
// -----------------------------------------------------------------------------

type MonitoringConfig struct {
	// +kubebuilder:default:=true
	Enabled bool `json:"enabled,omitempty"`
}

// -----------------------------------------------------------------------------
// Top-level Spec and Status
// -----------------------------------------------------------------------------

type CostManagementServiceConfigSpec struct {
	Global         GlobalConfig         `json:"global,omitempty"`
	Database       DatabaseConfig       `json:"database,omitempty"`
	Cache          CacheConfig          `json:"cache,omitempty"`
	Kafka          KafkaConfig          `json:"kafka,omitempty"`
	ObjectStorage  ObjectStorageConfig  `json:"objectStorage,omitempty"`
	Auth           AuthConfig           `json:"auth,omitempty"`
	RBAC           RBACConfig           `json:"rbac,omitempty"`
	CostManagement CostManagementConfig `json:"costManagement,omitempty"`
	ROS            ROSConfig            `json:"ros,omitempty"`
	Kruize         KruizeConfig         `json:"kruize,omitempty"`
	Ingress        IngressConfig        `json:"ingress,omitempty"`
	UI             UIConfig             `json:"ui,omitempty"`
	GatewayRoute   GatewayRouteConfig   `json:"gatewayRoute,omitempty"`
	Monitoring     MonitoringConfig     `json:"monitoring,omitempty"`
}

// Phase represents the overall installation/upgrade state.
// +kubebuilder:validation:Enum=Pending;Provisioning;Running;Degraded;Failed
type Phase string

const (
	PhasePending      Phase = "Pending"
	PhaseProvisioning Phase = "Provisioning"
	PhaseRunning      Phase = "Running"
	PhaseDegraded     Phase = "Degraded"
	PhaseFailed       Phase = "Failed"
)

type ComponentStatus struct {
	// +optional
	Ready bool `json:"ready,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
}

type ComponentStatuses struct {
	Database       ComponentStatus `json:"database,omitempty"`
	Cache          ComponentStatus `json:"cache,omitempty"`
	Migration      ComponentStatus `json:"migration,omitempty"`
	CostManagement ComponentStatus `json:"costManagement,omitempty"`
	ROS            ComponentStatus `json:"ros,omitempty"`
	RBAC           ComponentStatus `json:"rbac,omitempty"`
	Kruize         ComponentStatus `json:"kruize,omitempty"`
	Auth           ComponentStatus `json:"auth,omitempty"`
	UI             ComponentStatus `json:"ui,omitempty"`
}

type CostManagementServiceConfigStatus struct {
	// +kubebuilder:default=Pending
	Phase Phase `json:"phase,omitempty"`
	// Conditions summarises the current reconciliation state.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Per-component readiness breakdown.
	Components ComponentStatuses `json:"components,omitempty"`
	// Generation last observed during reconciliation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// -----------------------------------------------------------------------------
// Root object
// -----------------------------------------------------------------------------

// CostManagementServiceConfig is the schema for the on-premise Cost Management deployment.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=cmsc
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type CostManagementServiceConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CostManagementServiceConfigSpec   `json:"spec,omitempty"`
	Status CostManagementServiceConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type CostManagementServiceConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CostManagementServiceConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CostManagementServiceConfig{}, &CostManagementServiceConfigList{})
}
