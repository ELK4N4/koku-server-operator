package resources

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func testMonitoringCFG() *costv1alpha1.CostManagementServiceConfig {
	return &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management", Namespace: "cost-onprem"},
	}
}

func alertNamesFromRules(t *testing.T, pr *unstructured.Unstructured) map[string]bool {
	t.Helper()
	groups, found, err := unstructured.NestedSlice(pr.Object, "spec", "groups")
	if err != nil || !found || len(groups) == 0 {
		t.Fatalf("expected PrometheusRule groups, found=%v err=%v", found, err)
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("group[0] type %T", groups[0])
	}
	rules, ok := group["rules"].([]any)
	if !ok {
		t.Fatalf("rules type %T", group["rules"])
	}
	names := make(map[string]bool, len(rules))
	for _, r := range rules {
		rm, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("rule type %T", r)
		}
		name, _ := rm["alert"].(string)
		if name == "" {
			t.Fatal("rule missing alert name")
		}
		names[name] = true
	}
	return names
}

func TestPrometheusRules_BetaOperatorCentricSet(t *testing.T) {
	pr := PrometheusRules(testMonitoringCFG())
	if pr.GetName() != "cost-management-alerts" {
		t.Errorf("name: got %q", pr.GetName())
	}
	if pr.GetNamespace() != "cost-onprem" {
		t.Errorf("namespace: got %q", pr.GetNamespace())
	}

	names := alertNamesFromRules(t, pr)

	want := []string{
		"CostManagementMigrationFailed",
		"CostManagementMigrationStalled",
		"CostManagementDegraded",
		"CostManagementDependencyDown",
		"CostManagementPodRestarting",
		"CostManagementNotAvailable",
		"CostManagementAPIDown",
	}
	for _, a := range want {
		if !names[a] {
			t.Errorf("missing alert %s", a)
		}
	}

	absent := []string{
		"CostManagementCeleryBacklog",
		"CostManagementSchemaOutOfDate",
		"CostManagementNotProgressing",
	}
	for _, a := range absent {
		if names[a] {
			t.Errorf("unexpected alert %s (deferred or replaced)", a)
		}
	}
}

func TestAppServiceMonitor_BetaComponents(t *testing.T) {
	sm := AppServiceMonitor(testMonitoringCFG())
	if sm.GetName() != "cost-management-app-metrics" {
		t.Errorf("name: got %q", sm.GetName())
	}
	if sm.GroupVersionKind().Kind != "ServiceMonitor" {
		t.Errorf("kind: got %s", sm.GroupVersionKind().Kind)
	}

	endpoints, found, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
	if err != nil || !found || len(endpoints) != 1 {
		t.Fatalf("endpoints: found=%v len=%d err=%v", found, len(endpoints), err)
	}
	ep, ok := endpoints[0].(map[string]any)
	if !ok {
		t.Fatalf("endpoint type %T", endpoints[0])
	}
	if ep["port"] != "metrics" {
		t.Errorf("port: got %v want metrics", ep["port"])
	}
	if ep["path"] != "/metrics" {
		t.Errorf("path: got %v", ep["path"])
	}

	exprs, found, err := unstructured.NestedSlice(sm.Object, "spec", "selector", "matchExpressions")
	if err != nil || !found || len(exprs) != 1 {
		t.Fatalf("matchExpressions: found=%v len=%d err=%v", found, len(exprs), err)
	}
	expr, ok := exprs[0].(map[string]any)
	if !ok {
		t.Fatalf("expr type %T", exprs[0])
	}
	values, ok := expr["values"].([]any)
	if !ok {
		t.Fatalf("values type %T", expr["values"])
	}
	got := map[string]bool{}
	for _, v := range values {
		s, _ := v.(string)
		got[s] = true
	}
	for _, want := range []string{"cost-management-api", "cost-processor", "ingress"} {
		if !got[want] {
			t.Errorf("missing component %q", want)
		}
	}
	for _, absent := range []string{"listener", "ros-api", "ros-optimization", "gateway"} {
		if got[absent] {
			t.Errorf("unexpected component %q", absent)
		}
	}
}
