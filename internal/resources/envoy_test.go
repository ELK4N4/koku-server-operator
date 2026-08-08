package resources

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-server-operator/api/v1alpha1"
)

func testCfg() *costv1alpha1.CostManagementServiceConfig {
	return &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management", Namespace: "cost-onprem"},
		Spec: costv1alpha1.CostManagementServiceConfigSpec{
			Auth: costv1alpha1.AuthConfig{
				Keycloak: costv1alpha1.KeycloakSpec{
					URL:       "https://keycloak.keycloak.svc.cluster.local",
					Realm:     "kubernetes",
					Audiences: []string{"cost-management-operator", "cost-management-ui"},
				},
				Envoy: costv1alpha1.EnvoySpec{
					Replicas: 2,
					Image: costv1alpha1.ImageSpec{
						Repository: "registry.redhat.io/openshift-service-mesh/proxyv2-rhel9",
						Tag:        "2.6",
					},
				},
			},
		},
	}
}

func TestKeycloakIssuerAndJWKS(t *testing.T) {
	cfg := testCfg()
	wantIssuer := "https://keycloak.keycloak.svc.cluster.local/realms/kubernetes"
	if got := KeycloakIssuerURL(cfg); got != wantIssuer {
		t.Errorf("KeycloakIssuerURL = %q, want %q", got, wantIssuer)
	}
	wantJWKS := wantIssuer + "/protocol/openid-connect/certs"
	if got := KeycloakJWKSURL(cfg); got != wantJWKS {
		t.Errorf("KeycloakJWKSURL = %q, want %q", got, wantJWKS)
	}
}

func TestKeycloakIssuerURLOverrideKeepsInClusterJWKS(t *testing.T) {
	cfg := testCfg()
	cfg.Spec.Auth.Keycloak.URL = "http://keycloak-service.keycloak.svc.cluster.local:8080"
	cfg.Spec.Auth.Keycloak.IssuerURL = "https://keycloak.apps.example.com"
	cfg.Spec.Auth.Keycloak.Realm = "kubernetes"

	wantIssuer := "https://keycloak.apps.example.com/realms/kubernetes"
	if got := KeycloakIssuerURL(cfg); got != wantIssuer {
		t.Errorf("KeycloakIssuerURL = %q, want %q", got, wantIssuer)
	}
	wantJWKS := "http://keycloak-service.keycloak.svc.cluster.local:8080/realms/kubernetes/protocol/openid-connect/certs"
	if got := KeycloakJWKSURL(cfg); got != wantJWKS {
		t.Errorf("KeycloakJWKSURL = %q, want %q", got, wantJWKS)
	}

	yaml := EnvoyYAML(cfg)
	if !strings.Contains(yaml, "issuer: "+wantIssuer) {
		t.Errorf("EnvoyYAML missing issuer %q", wantIssuer)
	}
	if !strings.Contains(yaml, "uri: "+wantJWKS) {
		t.Errorf("EnvoyYAML missing JWKS uri %q", wantJWKS)
	}
	// JWKS cluster must target the in-cluster Service, not the public hostname.
	if !strings.Contains(yaml, "address: keycloak-service.keycloak.svc.cluster.local") {
		t.Error("EnvoyYAML JWKS cluster should use in-cluster Keycloak Service host")
	}
	if strings.Contains(yaml, "transport_socket:") {
		t.Error("in-cluster http JWKS should not enable upstream TLS")
	}

	// Full issuer override (includes /realms/) is used as-is.
	cfg.Spec.Auth.Keycloak.IssuerURL = "https://keycloak.apps.example.com/realms/custom"
	if got := KeycloakIssuerURL(cfg); got != "https://keycloak.apps.example.com/realms/custom" {
		t.Errorf("full IssuerURL = %q", got)
	}
}

func TestKeycloakDefaults(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"},
	}
	if got := KeycloakURL(cfg); got != defaultKeycloakURL {
		t.Errorf("KeycloakURL default = %q, want %q", got, defaultKeycloakURL)
	}
	if got := KeycloakRealm(cfg); got != defaultKeycloakRealm {
		t.Errorf("KeycloakRealm default = %q, want %q", got, defaultKeycloakRealm)
	}
	aud := KeycloakAudiences(cfg)
	if len(aud) != 2 || aud[0] != "cost-management-operator" {
		t.Errorf("KeycloakAudiences default = %v", aud)
	}
}

func TestEnvoyYAMLContainsIssuerAudiencesAndKokuCluster(t *testing.T) {
	cfg := testCfg()
	yaml := EnvoyYAML(cfg)

	checks := []string{
		"issuer: https://keycloak.keycloak.svc.cluster.local/realms/kubernetes",
		"uri: https://keycloak.keycloak.svc.cluster.local/realms/kubernetes/protocol/openid-connect/certs",
		"- cost-management-operator",
		"- cost-management-ui",
		"address: cost-management-koku-api.cost-onprem.svc.cluster.local",
		"port_value: 8000",
		"X-Rh-Identity",
		"X-Bearer-Token",
		"address: keycloak.keycloak.svc.cluster.local",
		"port_value: 443",
		"transport_socket:",
	}
	for _, want := range checks {
		if !strings.Contains(yaml, want) {
			t.Errorf("EnvoyYAML missing %q", want)
		}
	}
	for _, tok := range []string{"__HTTP_PORT__", "__ISSUER__", "__LUA__", "__KOKU_HOST__", "__KC_TLS__"} {
		if strings.Contains(yaml, tok) {
			t.Errorf("EnvoyYAML left unsubstituted token %q", tok)
		}
	}
}

func TestEnvoyYAMLHTTPKeycloakOmitsTLS(t *testing.T) {
	cfg := testCfg()
	cfg.Spec.Auth.Keycloak.URL = "http://keycloak.keycloak.svc.cluster.local:8080"
	yaml := EnvoyYAML(cfg)
	if strings.Contains(yaml, "transport_socket:") {
		t.Error("expected no TLS transport_socket for http:// Keycloak")
	}
	if !strings.Contains(yaml, "port_value: 8080") {
		t.Error("expected Keycloak port 8080")
	}
}

func TestEnvoyResourceNames(t *testing.T) {
	cfg := testCfg()
	cm := EnvoyConfigMap(cfg)
	if cm.Name != "cost-management-gateway-envoy-config" {
		t.Errorf("ConfigMap name = %q", cm.Name)
	}
	svc := EnvoyService(cfg)
	if svc.Name != "cost-management-gateway" {
		t.Errorf("Service name = %q", svc.Name)
	}
	if len(svc.Spec.Ports) != 2 || svc.Spec.Ports[0].Port != 80 {
		t.Errorf("Service ports = %+v", svc.Spec.Ports)
	}
	d := EnvoyDeployment(cfg)
	if d.Name != "cost-management-gateway" {
		t.Errorf("Deployment name = %q", d.Name)
	}
	if d.Spec.Template.Spec.Containers[0].Image != "registry.redhat.io/openshift-service-mesh/proxyv2-rhel9:2.6" {
		t.Errorf("image = %q", d.Spec.Template.Spec.Containers[0].Image)
	}
	if len(d.Spec.Template.Spec.InitContainers) != 1 || d.Spec.Template.Spec.InitContainers[0].Name != "prepare-ca-bundle" {
		t.Error("expected CA combine init container")
	}
}
