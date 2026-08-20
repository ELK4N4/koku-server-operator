package resources

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func TestPrometheusRules_APIDownTreatsAbsentUp(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management", Namespace: "cost-onprem"},
	}
	pr := PrometheusRules(cfg)
	expr := prometheusRuleExpr(t, pr, "CostManagementAPIDown")
	if !strings.Contains(expr, `up{job="cost-management-koku-api",namespace="cost-onprem"} == 0`) {
		t.Fatalf("APIDown missing up==0 clause: %s", expr)
	}
	if !strings.Contains(expr, `absent(up{job="cost-management-koku-api",namespace="cost-onprem"}) == 1`) {
		t.Fatalf("APIDown missing absent(up) clause (COST-8109): %s", expr)
	}
	if !strings.Contains(expr, " or ") {
		t.Fatalf("APIDown expected or of up==0 and absent(up): %s", expr)
	}
}

func prometheusRuleExpr(t *testing.T, pr *unstructured.Unstructured, alert string) string {
	t.Helper()
	groups, found, err := unstructured.NestedSlice(pr.Object, "spec", "groups")
	if err != nil || !found || len(groups) == 0 {
		t.Fatalf("expected PrometheusRule groups, found=%v err=%v", found, err)
	}
	g0, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("group[0] type %T", groups[0])
	}
	rules, ok := g0["rules"].([]any)
	if !ok {
		t.Fatalf("rules type %T", g0["rules"])
	}
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if rule["alert"] == alert {
			expr, _ := rule["expr"].(string)
			return expr
		}
	}
	t.Fatalf("alert %q not found", alert)
	return ""
}
