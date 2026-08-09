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

### I3. `PhaseError` is dead code — OPEN

No phase function returns a `PhaseError`. The `applyPhaseError` call in the
reconcile loop (line 151) never fires. `NewPhaseError` is exported but unused.

**Fix:** Either wrap phase errors at call sites (validation, migration at
minimum) or remove the `PhaseError` infrastructure.

### I4. Monitoring stage swallows all apply errors — OPEN

`internal/controller/costmanagementserviceconfig_controller.go:596-604` catches
all `apply` errors and logs them. RBAC, quota, or real API errors are silently
ignored.

**Fix:** Only skip on `meta.IsNoMatchError(err)` (CRD absent); return all
other errors.

### I5. `AppServiceMonitor` label selectors don't match actual pods — FIXED

`internal/resources/monitoring.go:56-59` selects components
`["koku-api", "masu", "listener", "ros-api", "ingress"]` but actual pod
labels are `"cost-management-api"`, `"cost-processor"`, `"listener"`,
`"ros-api"`, `"ingress"`. No metrics scraped from Koku API or Masu.

**Fix:** Use actual component label values.

### I6. `KruizeServiceMonitor` selects wrong component label — FIXED

`internal/resources/monitoring.go:64` selects `"kruize"` but actual label is
`"ros-optimization"`. Matches nothing.

### I7. No watches on Routes, ServiceMonitors, PrometheusRules, ClusterRoles — OPEN

`internal/controller/costmanagementserviceconfig_controller.go:758-771`
registers `Owns()` watches for core resources but not for unstructured types
(Routes, ServiceMonitors, PrometheusRules) or cluster-scoped resources
(ClusterRole, ClusterRoleBinding). A deleted Route means up to 5 minutes of
downtime before drift correction fires.

### I9. `CacheConfig.Auth.Enabled` is plain `bool`, not `*bool` — OPEN

Inconsistent with the documented `*bool` pattern for opt-out fields. Functionally
harmless since default is false, but may confuse future maintainers. Same for
`KafkaTLSSpec.Enabled`, `BootstrapAdminSpec.Enabled`, `KeycloakSyncSpec.Enabled`,
`CacheTLSSpec.Enabled`.

---

## Minor (Nice to Have)

### M1. `ubi9/ubi-minimal:9.7` hardcoded in 7+ places — OPEN

`internal/resources/volumes.go:128,149` and `internal/resources/ros.go:155,168,183,199`.
Should be a constant, and ideally a CR field for air-gapped environments.

### M3. `dbPodSC()` doc comment says "dbContainerSC" — OPEN

`internal/resources/database.go:172-174`.

### M4. DB name constants scattered — OPEN

`kruizeDBName` in `kruize.go`, `rosDBName` in `ros.go`, `rbacDBName` in
`migration.go`. Centralize in `names.go`.

### M6. Some containers don't drop ALL capabilities — OPEN

Inconsistent with `restricted-v2` SCC compliance — some containers omit
`Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}`.

---

## Recommendations

1. Add integration tests for the full `Reconcile()` loop (envtest)
2. Add a validation webhook to catch contradictions (e.g. `deploy: true` + external host)
3. Add `PodDisruptionBudgets` for critical-path workloads (Koku API, Envoy, RBAC)
4. Centralize init container image and DB name constants

---

## Verdict

**Not ready to merge — fix C1 and C2 first.**

C1 (NetworkPolicy port mismatch) will block all API traffic through the
gateway. C2 (MergeEnv) will silently ignore user env var overrides. The
ServiceMonitor label mismatches (I5, I6) will prevent metrics scraping from
all major services. After those four fixes, this is solid for an alpha release.
