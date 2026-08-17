package resources

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

var (
	serviceMonitorGVK = schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"}
	prometheusRuleGVK = schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "PrometheusRule"}
)

// serviceMonitor builds a ServiceMonitor that selects Services by component label.
func serviceMonitor(cfg *costv1alpha1.CostManagementServiceConfig, name, portName string, components []string) *unstructured.Unstructured {
	matchExpressions := make([]any, len(components))
	for i, c := range components {
		matchExpressions[i] = c
	}

	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	sm.SetName(name)
	sm.SetNamespace(cfg.Namespace)
	sm.SetLabels(Labels(cfg, "monitoring"))

	_ = unstructured.SetNestedSlice(sm.Object, []any{
		map[string]any{
			"port":     portName,
			"path":     "/metrics",
			"interval": "30s",
		},
	}, "spec", "endpoints")
	_ = unstructured.SetNestedField(sm.Object, map[string]any{
		"matchLabels": map[string]any{
			"app.kubernetes.io/managed-by": "koku-service-operator",
			"app.kubernetes.io/instance":   cfg.Name,
		},
		"matchExpressions": []any{
			map[string]any{
				"key":      "app.kubernetes.io/component",
				"operator": "In",
				"values":   matchExpressions,
			},
		},
	}, "spec", "selector")
	// Target only the CR's own namespace.
	_ = unstructured.SetNestedSlice(sm.Object, []any{cfg.Namespace}, "spec", "namespaceSelector", "matchNames")

	return sm
}

// AppServiceMonitor scrapes beta managed workloads that expose Prometheus
// /metrics on a Service port named "metrics": Koku API, Masu, and Ingress.
// Listener / ROS / Kruize / Gateway are intentionally excluded (no named metrics
// port, wrong path, or out of beta).
func AppServiceMonitor(cfg *costv1alpha1.CostManagementServiceConfig) *unstructured.Unstructured {
	return serviceMonitor(cfg, cfg.Name+"-app-metrics", "metrics", []string{
		"cost-management-api", "cost-processor", "ingress",
	})
}

// KruizeServiceMonitor watches Kruize which exposes metrics on port 8080.
// Not applied in beta; retained for ROS cleanup when ros.enabled flips false.
func KruizeServiceMonitor(cfg *costv1alpha1.CostManagementServiceConfig) *unstructured.Unstructured {
	return serviceMonitor(cfg, cfg.Name+"-kruize-metrics", "metrics", []string{"ros-optimization"})
}

// OperatorServiceMonitor watches the controller-manager metrics endpoint.
// Not applied in beta (scaffold HTTPS/port mismatch); retained for a later PR.
func OperatorServiceMonitor(cfg *costv1alpha1.CostManagementServiceConfig) *unstructured.Unstructured {
	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	sm.SetName(cfg.Name + "-operator-metrics")
	sm.SetNamespace(cfg.Namespace)
	sm.SetLabels(Labels(cfg, "monitoring"))

	_ = unstructured.SetNestedSlice(sm.Object, []any{
		map[string]any{
			"port":     "https",
			"path":     "/metrics",
			"interval": "30s",
			"scheme":   "https",
			"tlsConfig": map[string]any{
				"insecureSkipVerify": true,
			},
		},
	}, "spec", "endpoints")
	_ = unstructured.SetNestedStringMap(sm.Object, map[string]string{
		"control-plane": "controller-manager",
	}, "spec", "selector", "matchLabels")

	return sm
}

// PrometheusRules returns operator-centric alert rules plus scrape-up APIDown
// once App ServiceMonitor targets are wired (PR2).
func PrometheusRules(cfg *costv1alpha1.CostManagementServiceConfig) *unstructured.Unstructured {
	instance := cfg.Name
	ns := cfg.Namespace
	kokuAPIService := NameKokuAPI(cfg)

	rules := []any{
		// Migration Job failed
		map[string]any{
			"alert": "CostManagementMigrationFailed",
			"expr":  `kube_job_status_failed{namespace="` + ns + `",job_name=~"` + instance + `-(koku|ros|rbac)-migrate"} > 0`,
			"for":   "1m",
			"labels": map[string]any{
				"severity": "critical",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management migration job failed",
				"description": "Migration job {{ $labels.job_name }} has failed. Schema upgrades are blocked.",
			},
		},
		// Schema not up to date long enough → stalled migration / upgrade
		map[string]any{
			"alert": "CostManagementMigrationStalled",
			"expr": `kube_customresource_status_condition{` +
				`customresource_kind="CostManagementServiceConfig",` +
				`customresource_name="` + instance + `",` +
				`namespace="` + ns + `",` +
				`condition="SchemaUpToDate",status="false"} == 1`,
			"for": "10m",
			"labels": map[string]any{
				"severity": "warning",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management schema migration stalled",
				"description": "Database schema is not up to date for {{ $labels.customresource_name }} for more than 10 minutes.",
			},
		},
		// Operator degraded (condition)
		map[string]any{
			"alert": "CostManagementDegraded",
			"expr": `kube_customresource_status_condition{` +
				`customresource_kind="CostManagementServiceConfig",` +
				`customresource_name="` + instance + `",` +
				`namespace="` + ns + `",` +
				`condition="Degraded",status="true"} == 1`,
			"for": "5m",
			"labels": map[string]any{
				"severity": "critical",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management operator is degraded",
				"description": "The CostManagementServiceConfig {{ $labels.customresource_name }} has been in Degraded state for 5 minutes.",
			},
		},
		// Operator dependency validation failed (not BYOI exporter scrape)
		map[string]any{
			"alert": "CostManagementDependencyDown",
			"expr": `(` +
				`kube_customresource_status_condition{` +
				`customresource_kind="CostManagementServiceConfig",` +
				`customresource_name="` + instance + `",` +
				`namespace="` + ns + `",` +
				`condition="DatabaseReady",status="false"} == 1` +
				`) or (` +
				`kube_customresource_status_condition{` +
				`customresource_kind="CostManagementServiceConfig",` +
				`customresource_name="` + instance + `",` +
				`namespace="` + ns + `",` +
				`condition="CacheReady",status="false"} == 1` +
				`)`,
			"for": "5m",
			"labels": map[string]any{
				"severity": "critical",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management dependency validation failed",
				"description": "DatabaseReady or CacheReady is False on {{ $labels.customresource_name }} for more than 5 minutes (operator probe/validation).",
			},
		},
		// Managed pod restart storm (operator-owned pod name prefix)
		map[string]any{
			"alert": "CostManagementPodRestarting",
			"expr":  `increase(kube_pod_container_status_restarts_total{namespace="` + ns + `",pod=~"` + instance + `-.*"}[15m]) > 3`,
			"for":   "15m",
			"labels": map[string]any{
				"severity": "warning",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management managed pod restarting",
				"description": "Pod {{ $labels.pod }} in namespace {{ $labels.namespace }} restarted more than 3 times in 15 minutes.",
			},
		},
		// Stack not available
		map[string]any{
			"alert": "CostManagementNotAvailable",
			"expr": `kube_customresource_status_condition{` +
				`customresource_kind="CostManagementServiceConfig",` +
				`customresource_name="` + instance + `",` +
				`namespace="` + ns + `",` +
				`condition="Available",status="false"} == 1`,
			"for": "30m",
			"labels": map[string]any{
				"severity": "warning",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management stack is not available",
				"description": "CostManagementServiceConfig {{ $labels.customresource_name }} has Available=False for 30 minutes.",
			},
		},
		// Koku API /metrics scrape target down (requires App ServiceMonitor)
		map[string]any{
			"alert": "CostManagementAPIDown",
			"expr":  `up{namespace="` + ns + `",service="` + kokuAPIService + `"} == 0`,
			"for":   "5m",
			"labels": map[string]any{
				"severity": "critical",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management API metrics endpoint unreachable",
				"description": "Prometheus cannot scrape /metrics on Service {{ $labels.service }} in namespace {{ $labels.namespace }} for more than 5 minutes.",
			},
		},
	}

	pr := &unstructured.Unstructured{}
	pr.SetGroupVersionKind(prometheusRuleGVK)
	pr.SetName(cfg.Name + "-alerts")
	pr.SetNamespace(cfg.Namespace)
	pr.SetLabels(Labels(cfg, "monitoring"))

	_ = unstructured.SetNestedSlice(pr.Object, []any{
		map[string]any{
			"name":  "cost-management.rules",
			"rules": rules,
		},
	}, "spec", "groups")

	return pr
}
