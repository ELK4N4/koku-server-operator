# Code Review — 2026-08-09

Full-repo review of koku-service-operator at commit b848921.

---

## Strengths

- Phase-gated `runPhases` pipeline is clean and composable
- SSA with `ForceOwnership` done correctly; StatefulSet VCT workaround is pragmatic
- S3 discovery chain (user → OBC → NooBaa) well-structured and tested
- `restricted-v2` SCC compliance is solid — no `runAsUser`, documented exceptions
- Good test coverage on critical paths (discovery, S3, validation, ownership, envoy, routes)
- `MergeEnv` sorted-keys prevents SSA-induced rollouts
- Condition-based status API follows Kubernetes conventions properly

---

## Critical (Must Fix)

### C1. NetworkPolicy blocks Envoy → Koku API traffic — FIXED

`internal/resources/networkpolicies.go:138` allows ingress on port 9000 only,
but `KokuAPIService` exposes port 8000 and Envoy routes to port 8000. All
gateway traffic to the Koku API will be dropped by the NetworkPolicy.

**Fix:** Allow port 8000 in the NetworkPolicy (and optionally keep 9000 for
monitoring scrapes).

### C2. `MergeEnv` silently ignores user overrides — FIXED

`internal/resources/env.go:124-137` appends override entries without dedup.
Kubernetes uses the *first* occurrence, so user-provided env vars that
duplicate operator-set ones are silently ignored.

**Fix:** Filter base entries whose `Name` matches an override key before
appending.

---

## Important (Should Fix)

### I1. `asPhaseError` uses type assertion instead of `errors.As` — FIXED

`internal/controller/phases.go:71-80` does a direct `err.(*PhaseError)`
assertion. Wrapped errors will not be extracted. Use `errors.As` instead.

### I2. `httpProbe` leaks idle connections — FIXED

`internal/controller/validation.go:169-174` creates a new `http.Transport` +
`http.Client` on every reconcile loop. The transport is never closed.

**Fix:** Reuse a single client on the reconciler struct, or call
`transport.CloseIdleConnections()` after use.

### I3. `PhaseError` is dead code — DEFERRED

No phase function returns a `PhaseError`. The `applyPhaseError` call in the
reconcile loop (line 151) never fires. `NewPhaseError` is exported but unused.

**Decision:** Wire it up, not remove it. Refactor all 9 phases to return
`NewPhaseError` instead of inline `SetStatusCondition` + `fmt.Errorf`. This
centralizes condition-setting in `applyPhaseError` and eliminates boilerplate.
Separate ticket — touches all phase functions.

### I4. Monitoring stage swallows all apply errors — FIXED

`internal/controller/costmanagementserviceconfig_controller.go:596-604` caught
all `apply` errors and logged at Info level. Real failures were invisible.

**Fix:** Still non-blocking (monitoring must not prevent Ready), but now
distinguishes CRD-absent (`IsNoMatchError` → Info) from real failures
(RBAC, quota, API errors → Error level). The operator stays functional
either way, but real problems are now visible in logs.

### I5. `AppServiceMonitor` label selectors don't match actual pods — FIXED

`internal/resources/monitoring.go:56-59` selects components
`["koku-api", "masu", "listener", "ros-api", "ingress"]` but actual pod
labels are `"cost-management-api"`, `"cost-processor"`, `"listener"`,
`"ros-api"`, `"ingress"`. No metrics scraped from Koku API or Masu.

**Fix:** Use actual component label values.

### I6. `KruizeServiceMonitor` selects wrong component label — FIXED

`internal/resources/monitoring.go:64` selects `"kruize"` but actual label is
`"ros-optimization"`. Matches nothing.

### I7. No watches on Routes, ServiceMonitors, PrometheusRules, ClusterRoles — DEFERRED

`internal/controller/costmanagementserviceconfig_controller.go:758-771`
registers `Owns()` watches for core resources but not for unstructured types
(Routes, ServiceMonitors, PrometheusRules) or cluster-scoped resources
(ClusterRole, ClusterRoleBinding). A deleted Route means up to 5 minutes of
downtime before drift correction fires.

**Impact:** If one of these resources is deleted externally, the operator
won't notice until the 5-minute drift correction requeue (`requeueDrift`)
recreates it. No crash, no data loss — just delayed recovery.

**Decision:** Separate ticket. Unstructured CRDs (Routes, ServiceMonitors)
need discovery-gated dynamic watches — registering a watch for a missing GVK
crashes the controller at startup. Cluster-scoped resources (ClusterRole/
ClusterRoleBinding) can't use `Owns()` (ownerReferences don't cross scope);
need `Watches()` with a custom event handler mapping back to the CR.

### I9. `CacheConfig.Auth.Enabled` is plain `bool`, not `*bool` — WONTFIX

These are opt-in fields (default false). The `*bool` pattern is only needed
for opt-out fields (default true) where `omitempty` drops an explicit `false`.
For opt-in, `bool` + `omitempty` is correct — `false` and unset both mean
disabled. Changing to `*bool` would require updating 13 call sites for zero
functional benefit.

---

## Minor (Nice to Have)

### M1. `ubi9/ubi-minimal:9.7` hardcoded in 7+ places — FIXED

Extracted `UBIMinimalImage` constant in `names.go`. All 7 call sites in
`volumes.go`, `ros.go`, and `rbac.go` now reference the constant.

### M3. `dbPodSC()` doc comment says "dbContainerSC" — FIXED

`internal/resources/database.go` — moved misplaced comment to `dbContainerSC`.

### M4. DB name constants scattered — FIXED

Centralized `KokuDBName`, `RosDBName`, `RbacDBName`, `KruizeDBName` in
`names.go`. Per-file constants now reference the central definitions.

### M6. Some containers don't drop ALL capabilities — FIXED

Added `Capabilities: Drop: ["ALL"]` to `kokuAppContainerSC` (covers Koku API,
Masu, Listener, workers, migration), `restrictedContainerSC` (covers cache,
ingress, envoy, kruize), `rbacAppContainerSC`, and `dbContainerSC`.
All container security contexts now consistently comply with `restricted-v2`.

---

## Recommendations

1. Add integration tests for the full `Reconcile()` loop (envtest)
2. Add a validation webhook to catch contradictions (e.g. `deploy: true` + external host)
3. Add `PodDisruptionBudgets` for critical-path workloads (Koku API, Envoy, RBAC)
