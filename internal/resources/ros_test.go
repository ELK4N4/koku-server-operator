package resources

import (
	"strings"
	"testing"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func rosCfg() *costv1alpha1.CostManagementServiceConfig {
	cfg := testCfg()
	enabled := true
	deploy := false
	cfg.Spec.ROS.Enabled = &enabled
	cfg.Spec.ROS.Image.Repository = "quay.io/cloudservices/ros-ocp-backend"
	cfg.Spec.ROS.Image.Tag = "latest"
	cfg.Spec.Database.Deploy = &deploy
	cfg.Spec.Database.Host = "postgres.example.svc"
	cfg.Spec.Database.Port = 5432
	cfg.Spec.Kafka.BootstrapServers = "kafka.example.com:9092"
	return cfg
}

func TestROSServiceAccount(t *testing.T) {
	cfg := rosCfg()
	sa := ROSServiceAccount(cfg)
	if sa.Name != NameROSServiceAccount(cfg) {
		t.Errorf("Name = %q", sa.Name)
	}
	if sa.Namespace != cfg.Namespace {
		t.Errorf("Namespace = %q", sa.Namespace)
	}
}

func TestCdappConfigMap_ContainsDBAndKafka(t *testing.T) {
	cfg := rosCfg()
	cm := CdappConfigMap(cfg)
	if cm.Name != NameCdappConfigMap(cfg) {
		t.Errorf("Name = %q", cm.Name)
	}
	data := cm.Data["cdappconfig.json"]
	for _, want := range []string{
		`"hostname": "postgres.example.svc"`,
		`"name": "costonprem_ros"`,
		`"hostname": "kafka.example.com"`,
		`"port": 9092`,
		uploadTopic,
		recommendationTopic,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("cdappconfig missing %q\n%s", want, data)
		}
	}
}

func TestROSAPIDeployment_Shape(t *testing.T) {
	cfg := rosCfg()
	d := ROSAPIDeployment(cfg)
	if d.Name != NameROSAPI(cfg) {
		t.Errorf("Name = %q", d.Name)
	}
	if d.Spec.Template.Spec.ServiceAccountName != NameROSServiceAccount(cfg) {
		t.Errorf("SA = %q", d.Spec.Template.Spec.ServiceAccountName)
	}
	if len(d.Spec.Template.Spec.InitContainers) < 2 {
		t.Fatalf("expected DB+Kafka init containers, got %d", len(d.Spec.Template.Spec.InitContainers))
	}
	c := d.Spec.Template.Spec.Containers[0]
	if c.Image != "quay.io/cloudservices/ros-ocp-backend:latest" {
		t.Errorf("image = %q", c.Image)
	}
	if len(c.Ports) != 2 || c.Ports[0].ContainerPort != rosAPIPort || c.Ports[1].ContainerPort != rosMetricPort {
		t.Errorf("ports = %+v", c.Ports)
	}
	env := map[string]string{}
	for _, e := range c.Env {
		if e.Value != "" {
			env[e.Name] = e.Value
		}
	}
	if env["DB_NAME"] != rosDBName {
		t.Errorf("DB_NAME = %q", env["DB_NAME"])
	}
	if env["RBACHOST"] != NameRBACAPI(cfg) {
		t.Errorf("RBACHOST = %q", env["RBACHOST"])
	}
	if env["SERVICE_NAME"] != "ros-api" {
		t.Errorf("SERVICE_NAME = %q", env["SERVICE_NAME"])
	}
	if c.LivenessProbe == nil || c.LivenessProbe.HTTPGet == nil || c.LivenessProbe.HTTPGet.Path != "/status" {
		t.Errorf("liveness probe = %+v", c.LivenessProbe)
	}
}

func TestROSAPIService_Ports(t *testing.T) {
	cfg := rosCfg()
	svc := ROSAPIService(cfg)
	if svc.Name != NameROSAPI(cfg) {
		t.Errorf("Name = %q", svc.Name)
	}
	if len(svc.Spec.Ports) != 2 || svc.Spec.Ports[0].Port != rosAPIPort || svc.Spec.Ports[1].Port != rosMetricPort {
		t.Errorf("ports = %+v", svc.Spec.Ports)
	}
}

func TestROSProcessorDeployment_Shape(t *testing.T) {
	cfg := rosCfg()
	cfg.Spec.ROS.Processor.Replicas = 2
	cfg.Spec.ROS.Processor.LogLevel = "DEBUG"
	d := ROSProcessorDeployment(cfg)
	if d.Name != NameROSProcessor(cfg) {
		t.Errorf("Name = %q", d.Name)
	}
	if d.Spec.Replicas == nil || *d.Spec.Replicas != 2 {
		t.Errorf("replicas = %v", d.Spec.Replicas)
	}
	c := d.Spec.Template.Spec.Containers[0]
	env := map[string]string{}
	for _, e := range c.Env {
		if e.Value != "" {
			env[e.Name] = e.Value
		}
	}
	if env["SERVICE_NAME"] != "ros-processor" {
		t.Errorf("SERVICE_NAME = %q", env["SERVICE_NAME"])
	}
	if env["UPLOAD_TOPIC"] != uploadTopic {
		t.Errorf("UPLOAD_TOPIC = %q", env["UPLOAD_TOPIC"])
	}
	if env["LOG_LEVEL"] != "DEBUG" {
		t.Errorf("LOG_LEVEL = %q", env["LOG_LEVEL"])
	}
	if env["KRUIZE_HOST"] != NameKruize(cfg) {
		t.Errorf("KRUIZE_HOST = %q", env["KRUIZE_HOST"])
	}
	// Must wait for DB, Kafka, and Kruize before starting.
	if len(d.Spec.Template.Spec.InitContainers) < 3 {
		t.Fatalf("expected ≥3 init containers, got %d", len(d.Spec.Template.Spec.InitContainers))
	}
}
