package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func servicePortsByName(ports []corev1.ServicePort) map[string]corev1.ServicePort {
	out := make(map[string]corev1.ServicePort, len(ports))
	for _, p := range ports {
		out[p.Name] = p
	}
	return out
}

func TestKokuAPIService(t *testing.T) {
	cfg := testCfg()
	svc := KokuAPIService(cfg)
	if svc.Name != NameKokuAPI(cfg) {
		t.Errorf("Name = %q, want %q", svc.Name, NameKokuAPI(cfg))
	}
	if svc.Namespace != cfg.Namespace {
		t.Errorf("Namespace = %q", svc.Namespace)
	}
	if svc.Spec.Selector[labelComponent] != "cost-management-api" {
		t.Errorf("selector component = %q", svc.Spec.Selector[labelComponent])
	}
	if len(svc.Spec.Ports) != 2 {
		t.Fatalf("ports = %+v, want http + metrics", svc.Spec.Ports)
	}

	byName := servicePortsByName(svc.Spec.Ports)
	httpPort, ok := byName["http"]
	if !ok {
		t.Fatal("missing port named http")
	}
	if httpPort.Port != 8000 || httpPort.Protocol != corev1.ProtocolTCP {
		t.Errorf("http port = %+v, want port 8000/TCP", httpPort)
	}
	if httpPort.TargetPort != intstr.FromString("http") {
		t.Errorf("http TargetPort = %+v, want named http", httpPort.TargetPort)
	}

	metricsPort, ok := byName["metrics"]
	if !ok {
		t.Fatal("missing port named metrics")
	}
	if metricsPort.Port != 9000 || metricsPort.Protocol != corev1.ProtocolTCP {
		t.Errorf("metrics port = %+v, want port 9000/TCP", metricsPort)
	}
	if metricsPort.TargetPort != intstr.FromString("metrics") {
		t.Errorf("metrics TargetPort = %+v, want named metrics", metricsPort.TargetPort)
	}
}

func TestMasuService(t *testing.T) {
	cfg := testCfg()
	svc := MasuService(cfg)
	if svc.Name != NameMasu(cfg) {
		t.Errorf("Name = %q, want %q", svc.Name, NameMasu(cfg))
	}
	if svc.Namespace != cfg.Namespace {
		t.Errorf("Namespace = %q", svc.Namespace)
	}
	if svc.Spec.Selector[labelComponent] != "cost-processor" {
		t.Errorf("selector component = %q", svc.Spec.Selector[labelComponent])
	}
	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("ports = %+v", svc.Spec.Ports)
	}

	byName := servicePortsByName(svc.Spec.Ports)
	port, ok := byName["metrics"]
	if !ok {
		t.Fatal("missing port named metrics")
	}
	// Named "metrics" so App ServiceMonitor can scrape; callers still dial :9000.
	if port.Port != 9000 || port.Protocol != corev1.ProtocolTCP {
		t.Errorf("port = %+v, want metrics/9000/TCP", port)
	}
	if port.TargetPort != intstr.FromString("metrics") {
		t.Errorf("TargetPort = %+v, want named metrics", port.TargetPort)
	}
}
