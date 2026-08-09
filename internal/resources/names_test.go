package resources

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func TestDNS1123Label(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"cost_model", "cost-model"},
		{"subs_extraction", "subs-extraction"},
		{"subs_transmission", "subs-transmission"},
		{"celery", "celery"},
		{"ocp", "ocp"},
	}
	for _, tt := range tests {
		if got := DNS1123Label(tt.in); got != tt.want {
			t.Errorf("DNS1123Label(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNameCeleryWorkerSanitizesQueue(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management"},
	}
	got := NameCeleryWorker(cfg, "cost_model")
	want := "cost-management-celery-worker-cost-model"
	if got != want {
		t.Errorf("NameCeleryWorker() = %q, want %q", got, want)
	}
}

func TestCeleryWorkerDeploymentKeepsCeleryQueue(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management", Namespace: "cost-onprem"},
	}
	cfg.Spec.CostManagement.API.Image.Repository = "quay.io/example/koku"
	cfg.Spec.CostManagement.API.Image.Tag = "latest"

	d := CeleryWorkerDeployment(cfg, "cost_model", costv1alpha1.CeleryWorkerSpec{Replicas: 1})
	if d.Name != "cost-management-celery-worker-cost-model" {
		t.Errorf("Deployment.Name = %q, want cost-management-celery-worker-cost-model", d.Name)
	}
	if d.Spec.Template.Spec.Containers[0].Name != "cost-worker-cost-model" {
		t.Errorf("container name = %q, want cost-worker-cost-model", d.Spec.Template.Spec.Containers[0].Name)
	}

	var queues string
	for _, e := range d.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "WORKER_QUEUES" {
			queues = e.Value
			break
		}
	}
	if queues != "cost_model" {
		t.Errorf("WORKER_QUEUES = %q, want cost_model (Celery queue must keep underscore)", queues)
	}
}
