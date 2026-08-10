# Helm Chart vs Operator Comparison Report

Date: 2026-08-09

Systematic comparison of `cost-onprem-chart/cost-onprem/` (Helm chart) against
`koku-service-operator` (operator) — identifying deviations, missing pieces,
and bugs.

Both projects deploy the same Cost Management on-premise stack: Koku API,
Masu, Listener, Celery workers, RBAC, ROS, Kruize, Ingress, Envoy gateway,
and UI. The Helm chart uses `values.yaml` + Go templates; the operator uses
a `CostManagementServiceConfig` CR + Go reconciler.

---

## 1. Resource Inventory

| Component | Helm Chart | Operator | Status |
|-----------|-----------|----------|--------|
| PostgreSQL StatefulSet + Service | yes | yes | match |
| Valkey Deployment + PVC + Service | yes | yes | match |
| DB Credentials Secret | install script | auto-generated | operator better |
| Django Secret | install script | auto-generated | operator better |
| Storage Credentials Secret | install script | auto-generated (placeholder) | operator better |
| DB Init ConfigMap | yes | yes | match |
| AWS Config ConfigMap | yes | yes | match |
| CA Combine ConfigMap | yes | yes | match |
| Service CA ConfigMap | yes | yes | match |
| Koku API Deployment + Service | yes | yes | match |
| Masu Deployment + Service | yes | yes | **port mismatch** |
| Listener Deployment | yes | yes | match |
| Koku ServiceAccount | yes | yes | match |
| Koku Migration Job | yes | yes | match |
| Celery Beat Deployment | yes | yes | **no resources** |
| Celery Workers (10 queues) | yes | yes | match |
| RBAC API Deployment + Service | yes | yes | match |
| RBAC Worker Deployment | yes | yes | match |
| RBAC Migration Job | yes | yes | operator has richer seeding |
| RBAC Admin Bootstrap Job | yes | yes | match |
| RBAC Keycloak Sync CronJob | yes | **MISSING** | **gap** |
| RBAC ServiceMonitor | yes | **MISSING** | **gap** |
| ROS API Deployment + Service | yes | yes | match |
| ROS Processor Deployment | yes | yes | match |
| ROS Processor Service | yes | **MISSING** | **gap** |
| ROS Recommendation Poller Deployment | yes | yes | match |
| ROS Recommendation Poller Service | yes | **MISSING** | **gap** |
| ROS Housekeeper Deployment | yes | yes | match |
| ROS Migration Job | yes | yes | match |
| ROS Partition Cleaner CronJob | yes | yes | match |
| ROS ServiceAccount | yes | yes | match |
| ROS NetworkPolicies | yes | **MISSING** | **gap** |
| Cdapp ConfigMap (ROS/Kruize) | yes | yes | match |
| Kruize Deployment + Service | yes | yes | match |
| Kruize ServiceAccount | yes | yes | match |
| Kruize ClusterRole + ClusterRoleBinding | yes | yes | match |
| Kruize ConfigMap | yes | yes | match |
| Kruize Partition CronJob | yes | yes | match |
| Kruize NetworkPolicy | yes | yes | match |
| Ingress Deployment + Service | yes | yes | match |
| Ingress NetworkPolicy | yes | yes | match |
| Envoy Gateway Deployment + Service | yes | yes | match |
| Envoy ConfigMap | yes | yes | routing differences |
| Gateway CA ConfigMap | yes | handled via combined CA | equivalent |
| Gateway NetworkPolicy | yes | yes | match |
| Gateway Route | yes | yes | path difference |
| UI Deployment (oauth-proxy + nginx) | yes | yes | match |
| UI Service | yes | yes | match |
| UI nginx ConfigMap | yes | yes | match |
| UI Route | yes | yes | match |
| UI ConsoleLink | yes | yes | match |
| Koku ServiceMonitor | yes | yes | match |
| Kruize ServiceMonitor | yes | yes | match |
| **PrometheusRules (5 alert rules)** | **no** | **yes** | **operator better** |
| Koku API NetworkPolicy | no | yes | operator better |
| RBAC API NetworkPolicy | no | yes | operator better |
| Keycloak Debug ConfigMap | yes | no | not needed |

---

## 2. Critical Issues (Broken / Wrong)

### 2.1 Envoy ingress upload timeout too low

**Severity: HIGH — will cause 504 errors on file uploads**

The Helm chart configures the ingress (upload) route with a 180-second timeout
and 60-second per-try timeout specifically because uploads are large and slow
(PERF-FINDING-001):

```yaml
# Helm chart values.yaml
jwtAuth.envoy.ingressTimeout: "180s"
jwtAuth.envoy.ingressPerTryTimeout: "60s"
```

The operator's Envoy config uses a flat 30s timeout for `/api/ingress/`:

```yaml
# internal/resources/envoy.go — envoyYAMLTemplate, /api/ingress/ route
- match:
    prefix: "/api/ingress/"
  route:
    cluster: ingress-backend
    timeout: 30s
    retry_policy:
      per_try_timeout: 15s
```

The chart's Envoy template (line 160) uses the configurable values:

```yaml
timeout: {{ .Values.jwtAuth.envoy.ingressTimeout | default "30s" }}
per_try_timeout: {{ .Values.jwtAuth.envoy.ingressPerTryTimeout | default "10s" }}
```

With `values.yaml` setting `ingressTimeout: "180s"` and
`ingressPerTryTimeout: "60s"`.

**This will cause 504 Gateway Timeout on any upload taking longer than 30s.**
The fix is to use 180s / 60s matching the chart, or make them configurable
from the CR.

### 2.2 Celery beat has zero resource limits

**Severity: MEDIUM — unbounded resource consumption**

`CeleryBeatDeployment()` in `koku.go:133` passes `corev1.ResourceRequirements{}`
(empty). The chart sets:

```yaml
requests: { cpu: 50m, memory: 200Mi }
limits:   { cpu: 100m, memory: 400Mi }
```

Without limits, beat can consume unbounded resources and starve other pods
on the node. It won't be evicted during memory pressure either.

### 2.3 Masu Service port mismatch

**Severity: MEDIUM — metrics scraping may break**

Operator `MasuService()` exposes port **9000** (the metrics port):
```go
return appService(cfg, NameMasu(cfg), "cost-processor", 9000)
```

The Helm chart's Masu service exposes port **8000** (the Gunicorn HTTP port).
The chart distinguishes API port (8000) from metrics port (9000) in
`_helpers-koku.tpl` lines 70-78.

The operator's `AppServiceMonitor` tries to scrape the `"metrics"` named port,
but the service only has one port named `"http"` on 9000 — so the port name
doesn't match either.

This should be a two-port service (8000 for http, 9000 for metrics) or at
minimum match the chart's port 8000.

### 2.4 RBAC Keycloak Sync CronJob not implemented

**Severity: HIGH — feature gap for multi-org deployments**

The CR type defines `KeycloakSyncSpec` with schedule, orgGroupPrefix,
orgAdminSubgroup, pruneOrphans, clientId, clientSecretRef — but **no
reconciler code creates the CronJob**. The chart has a full
`rbac/cronjob-sync.yaml` template.

This means multi-org Keycloak-to-RBAC user sync won't work with the operator.
Users added/disabled in Keycloak won't propagate to RBAC.

---

## 3. Missing Resources (Gaps)

### 3.1 ROS Processor and Recommendation Poller Services

The chart creates Service objects for both with a metrics port (9000),
enabling Prometheus scraping. The operator creates Deployments for both
but no Services. The `AppServiceMonitor` can't scrape what isn't exposed.

### 3.2 ROS NetworkPolicies

The chart has 6 NetworkPolicies in `ros/networkpolicies.yaml`:
- `ros-api-metrics` — allows Prometheus to scrape ROS API metrics (port 9000)
- `cost-api-metrics` — allows Prometheus to scrape Koku API metrics (port 9000)
- `processor-metrics` — allows Prometheus to scrape ROS Processor metrics (port 9000)
- `poller-metrics` — allows Prometheus to scrape Recommendation Poller metrics (port 9000)
- `ros-api-access` — allows gateway to reach ROS API (port 8000)
- `cost-api-access` — allows gateway, ingress, and housekeeper to reach Koku API (port 8000)

The operator has `KokuAPINetworkPolicy` (covering gateway/masu/housekeeper
to koku-api) but is missing the ROS-specific policies and the Prometheus
metrics scraping policies. Any pod in the namespace can call the ROS
services, and Prometheus may have trouble scraping metrics if other
NetworkPolicies impose a default-deny.

### 3.3 RBAC ServiceMonitor

The chart has `rbac/servicemonitor.yaml` to scrape RBAC API metrics.
The operator's `AppServiceMonitor` only covers: koku-api, masu, listener,
ros-api, ingress. RBAC metrics go unscraped.

---

## 4. Env Var Differences

### 4.1 Koku env vars: chart sets defaults, operator relies on CR `.env` merge

The Helm chart explicitly sets these env vars with defaults in `values.yaml`
under `costManagement.api.env`. The operator does NOT set them, relying on
users to provide them via `spec.costManagement.api.env`:

| Env Var | Chart Default | Operator | Impact |
|---------|---------------|----------|--------|
| `DEVELOPMENT` | `"False"` | not set | koku defaults to "True" in dev? |
| `KOKU_ENABLE_SENTRY` | `"False"` | not set | Sentry SDK may try to phone home |
| `CACHED_VIEWS_DISABLED` | `"False"` | not set | probably fine (app default) |
| `RETAIN_NUM_MONTHS` | `"3"` | not set | koku default may differ |
| `NOTIFICATION_CHECK_TIME` | `"24"` | not set | probably fine |
| `ENHANCED_ORG_ADMIN` | `"False"` | not set | **critical for RBAC scoping** |
| `RBAC_CACHE_TIMEOUT` | `"300"` | not set | probably fine |
| `CACHE_TIMEOUT` | `"3600"` | not set | probably fine |
| `TAG_ENABLED_LIMIT` | `"200"` | not set | probably fine |
| `USE_READREPLICA` | `"False"` | not set | probably fine |
| `KOKU_LOG_LEVEL` | `"INFO"` | not set | noisy defaults? |
| `DJANGO_LOG_LEVEL` | `"INFO"` | not set | noisy defaults? |
| `DJANGO_LOG_FORMATTER` | `"simple"` | not set | probably fine |
| `GUNICORN_LOG_LEVEL` | `"INFO"` | not set | probably fine |
| `INITIAL_INGEST_NUM_MONTHS` | `"2"` | not set | may over-ingest |
| `INITIAL_INGEST_OVERRIDE` | `"False"` | not set | probably fine |
| `S3_VERIFY_SSL` | `"false"` | not set | may fail on self-signed S3 |

**`ENHANCED_ORG_ADMIN`** is particularly important: when True, Koku treats
all org_admin users as having full access without checking RBAC, which
defeats per-org admin scoping. The chart's keycloakSync template even
validates this at render time (cronjob-sync.yaml line 3-5):

```yaml
{{- $enhancedOrgAdmin := index .Values.costManagement.api.env "ENHANCED_ORG_ADMIN" | default "False" -}}
{{- if ne (lower $enhancedOrgAdmin) "false" }}
  {{- fail "rbac.keycloakSync requires ENHANCED_ORG_ADMIN=False..." -}}
```

The operator should hardcode this to `"False"`.

### 4.2 Masu env vars missing

| Env Var | Chart Default | Operator |
|---------|---------------|----------|
| `INITIAL_INGEST_NUM_MONTHS` | `"2"` | not set |
| `INITIAL_INGEST_OVERRIDE` | `"False"` | not set |
| `RETAIN_NUM_MONTHS` | `"3"` | not set |

These control how much historical data Masu processes. Without them,
the koku defaults apply (which may not match intended on-prem behavior).

### 4.3 Missing `POLLING_TIMER` env var

The chart sets `POLLING_TIMER` (Celery polling interval) from
`costManagement.celery.pollingTimer` (default: 86400 = 24h via
`_helpers-koku.tpl` line 380). The operator does not set this env var.
The koku app default may differ.

### 4.4 RBAC env vars: `ROLE_CREATE_ALLOW_LIST`

The chart exposes `rbac.roleCreateAllowList` which maps to the
`ROLE_CREATE_ALLOW_LIST` env var. The operator doesn't expose this.

### 4.5 Both use the same pattern for user-configurable env vars

The chart iterates `values.costManagement.api.env` with `range $key, $value`
and injects each as a plain env var. The operator's `MergeEnv()` function
does the same with `spec.costManagement.api.env`. So the env vars listed
in section 4.1 are not missing *code* — they are missing sensible *defaults*
in the CR. The chart ships them in `values.yaml`; the operator should set
them in `KokuCommonEnv()` or document them in the CR defaults.

---

## 5. Good Deviations (Operator is Better)

### 5.1 PrometheusRules with 5 alert rules

The operator creates a `PrometheusRule` with (`monitoring.go`):
- `CostManagementMigrationFailed` — critical, fires on failed migration jobs
- `CostManagementDegraded` — critical, fires when Degraded condition is true for 5m
- `CostManagementSchemaOutOfDate` — warning, fires when SchemaUpToDate is false for 15m
- `CostManagementAPIDown` — critical, fires when koku-api metrics are unreachable for 5m
- `CostManagementNotProgressing` — warning, fires when Available is false for 30m

The chart has none of these. This is strictly better for day-2 operations.

### 5.2 Auto-discovery phase

The operator auto-detects (`discovery.go`, `discovery_s3.go`):
- Cluster domain (from OpenShift Ingress config)
- Default StorageClass
- S3 endpoint + credentials (from OBC/NooBaa)

The chart relies on the install script passing these as `--set` overrides.
The operator approach is more robust and self-healing.

### 5.3 Phased reconciliation with readiness gates

The operator won't deploy services until the database is ready, won't
start workers until the API is healthy, and won't create Routes until
the cluster domain is resolved (`costmanagementserviceconfig_controller.go`
stages 1-7). The chart just deploys everything at once and relies on init
containers for ordering.

### 5.4 Comprehensive RBAC migration + seeding Job

The operator's RBAC migration Job does more than the chart's (`migration.go`):
- Django migrations
- Built-in seeds
- Cost-management permission + role seeding (Cost Administrator, Price List Admin, etc.)
- admin_default group creation per tenant
- bootstrap_tenants
- platform_default cleanup

This is a superset of what the chart achieves with separate migration
and bootstrap jobs.

### 5.5 Image-tag-based migration re-run

The operator annotates migration Jobs with the image tag. When the image
changes, it deletes the old Job and creates a new one — automatic upgrade
migrations (`costmanagementserviceconfig_controller.go:350`). The chart
relies on Helm pre-upgrade hooks.

### 5.6 Drift correction every 5 minutes

The operator re-applies desired state on a 5-minute interval, reverting
manual edits to managed resources (`requeueDrift = 5 * time.Minute`).
Helm only applies on install/upgrade.

### 5.7 Auto-generated secrets with secure random passwords

The operator generates DB credentials, Django secret key, and storage
credentials with 32-character random passwords (`secrets.go`). The chart
relies on the install script to create them before `helm install`.

---

## 6. Neutral Deviations

### 6.1 Envoy routing config: largely equivalent

Both the chart and operator define the same 5 Envoy route entries:

| Route prefix | Cluster | Chart timeout | Operator timeout |
|---|---|---|---|
| `/api/cost-management/v1/recommendations/openshift` | ros-api-backend | 30s | 30s |
| `/api/rbac/` | rbac-api-backend | 30s | 30s |
| `/api/cost-management/` | koku-api-backend | 60s | 60s |
| `/api/ingress/ready` | ingress-backend | 10s | 10s |
| `/api/ingress/` | ingress-backend | **180s** | **30s** |

The JWT filter, Lua X-Rh-Identity injection, and cluster definitions are
functionally identical. The only material difference is the ingress timeout
(see section 2.1).

### 6.2 Route path: `/api` vs `/`

Operator creates the gateway Route with `spec.path: /api`. The chart
creates a broader Route at `/`. This is correct — the gateway only
handles API traffic; the UI has its own Route via passthrough TLS.

### 6.3 Two separate Routes (API + UI) vs one

The operator creates two Routes: one for the API gateway (edge TLS) and
one for the UI (passthrough TLS to oauth2-proxy). The chart does the same.
Architecturally sound.

### 6.4 Labels

Operator uses `app.kubernetes.io/{name,instance,component,managed-by}`.
Chart uses Helm-standard labels. Both are valid; the operator's labels
are more Kubernetes-native.

### 6.5 Gateway Route timeout annotation not set by default

The operator doesn't set `haproxy.router.openshift.io/timeout` on the
gateway Route by default (the CR allows annotations via
`spec.gatewayRoute.annotations`). The chart sets `180s`.

This isn't technically a bug because the user can set it, but the default
should be 180s for upload compatibility. Combined with the Envoy timeout
issue in 2.1, uploads are doubly broken.

---

## 7. Recommended Fixes (Priority Order)

1. **Fix Envoy ingress timeout** (`envoy.go`): change `/api/ingress/` route timeout to 180s and per_try_timeout to 60s
2. **Add Celery beat resources** (`koku.go`): set `{cpu: 50m, mem: 200Mi}` / `{cpu: 100m, mem: 400Mi}`
3. **Fix Masu Service port** (`koku.go`): expose port 8000 (http) + 9000 (metrics), or match chart
4. **Implement RBAC Keycloak Sync CronJob**: the CR type exists, just needs reconciler code
5. **Add ROS Processor + Poller Services**: needed for Prometheus metrics scraping
6. **Add ROS NetworkPolicies**: restrict traffic to ROS components
7. **Set critical env var defaults**: at minimum `ENHANCED_ORG_ADMIN=False`, `RETAIN_NUM_MONTHS=3`, `S3_VERIFY_SSL=false`
8. **Add RBAC to AppServiceMonitor** components list
9. **Set default Route timeout annotation** to 180s in GatewayAPIRoute
10. **Expose `roleCreateAllowList`** in the RBAC CR section
