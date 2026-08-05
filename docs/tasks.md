# Operator Task Tracker

Tracks implementation status against the COST-7678–7700 Jira backlog.
Last audited: 2026-08-05 against `~/operator/COST-*.md` source files.

## Legend
- ✅ Done — implements the ticket's acceptance criteria
- 🔄 In Progress — partially implemented, specific gaps noted
- ❌ Not started

---

## CRD & API

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| [COST-7678](https://redhat.atlassian.net/browse/COST-7678) | Define CostManagement CRD types | 🔄 | Single file `api/v1alpha1/costmanagementserviceconfig_types.go` — ticket requires split across `infra_types.go`, `app_types.go`, `status_types.go`, `profiles.go`, `defaults.go`, `validation.go`. Phase enum differs: ticket defines Pending/Discovering/Validating/Migrating/Deploying/Ready/Degraded; we have Pending/Provisioning/Running/Degraded/Failed. Ticket is external-only for infra; we add `Deploy: true` bundled mode as an intentional extension. Missing: `profiles` (standard/ha), `DiscoveredConfig` in status, condition types DiscoveryComplete/StorageReady/DatabaseReady/SchemaUpToDate/AuthenticationReady, mutating/validating webhooks. |
| [COST-7679](https://redhat.atlassian.net/browse/COST-7679) | Create sample CRs and generate manifests | 🔄 | Single sample CR present; ticket requires two: minimal (required fields only) and production (HA profile, full resource overrides, monitoring enabled). CRD installs on CRC (OCP 4.21) ✅. Missing: HA profile sample, verified CEL validation. |

## Reconciler Core

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| [COST-7680](https://redhat.atlassian.net/browse/COST-7680) | Implement phase-gated reconciler skeleton | 🔄 | `runPhases()` + `PhaseError` pattern in place ✅. Ticket's 5 phases: Discovery → Validation → Migrations → Application → Platform. We have 6 stages that roughly map but are named differently and missing: Discovery stage (auto-detect cluster domain/StorageClass), Validation stage (probe external deps), pause/resume via annotation, Kubernetes Events on state transitions. |
| [COST-7681](https://redhat.atlassian.net/browse/COST-7681) | Implement Server-Side Apply and ownership model | 🔄 | SSA with `ForceOwnership` ✅, `ownerReferences` on namespace-scoped resources ✅. BYOI: all six dependency types (DB, Cache, Kafka, S3, Auth, ObjectStorage) fully wired — Kafka SASL/TLS env vars and volumes added. Missing: finalizer-based cleanup for cluster-scoped resources (ConsoleLink, ClusterRole, ClusterRoleBinding), drift correction (5-minute periodic requeue re-applying all desired state). |
| [COST-7682](https://redhat.atlassian.net/browse/COST-7682) | Implement cluster discovery | ❌ | Cluster domain and default StorageClass auto-detection from OpenShift cluster config not implemented. `DiscoveryComplete` condition and `status.discoveredConfig` not present. |
| [COST-7683](https://redhat.atlassian.net/browse/COST-7683) | Implement S3 backend auto-detection | ❌ | Three-path S3 resolution (user-provided → OBC/Direct Ceph → NooBaa) not implemented. Placeholder storage secret created but no real detection. `StorageReady` condition and `status.discoveredConfig.s3` not present. |
| [COST-7684](https://redhat.atlassian.net/browse/COST-7684) | Implement external dependency validation | ❌ | No connectivity probes for DB, Cache, Kafka, OIDC, or S3. No secret resolution/validation (keys exist, expected keys present). Missing conditions: DatabaseReady, CacheReady, KafkaReady, AuthenticationReady. |

## Infrastructure

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| [COST-7685](https://redhat.atlassian.net/browse/COST-7685) | Implement migration Job lifecycle | 🔄 | Koku migration Job created and gated ✅, upgrade-detection by image tag ✅, `Result{Stop:true}` on failure ✅. Missing: ROS and RBAC migration Jobs (ticket: sequential Koku → ROS → RBAC migrate → RBAC seed), `activeDeadlineSeconds: 600` ❌ (not set), `backoffLimit: 3` ❌ (we use 0), `SchemaUpToDate` condition ❌ (we use `condDegraded`). |

## Application Services

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| [COST-7686](https://redhat.atlassian.net/browse/COST-7686) | Implement application services | 🔄 | Koku API, Masu, Listener `1/1 Running` on CRC ✅. Missing per ticket: ROS API + Processor Deployments, Kruize Deployment + ClusterRole/ClusterRoleBinding with finalizer. Django key uses `base64.URLEncoding` ❌ — ticket requires `crypto/rand` with charset `abcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*(-_=+)`. Profile-based sizing ❌. 5-minute readiness timeout with Degraded condition ❌. Bundled DB/Cache (`Deploy: true`) is an intentional extension beyond ticket scope. |
| [COST-7687](https://redhat.atlassian.net/browse/COST-7687) | Implement workers and scheduled jobs | 🔄 | Celery Beat ✅. Ticket specifies six on-prem queues: download, summary, cost_model, refresh, ocp, priority — we have all of these plus extra (default, hcs, subs_extraction, subs_transmission). Missing: ROS Recommendation Poller Deployment, ROS Housekeeper Deployment, ROS Partition Cleaner CronJob, Kruize CronJobs, profile-based sizing. |
| [COST-7688](https://redhat.atlassian.net/browse/COST-7688) | Implement Gateway and Ingress | ❌ | Envoy JWT proxy (Deployment + Service + ConfigMap wired to OIDC issuer/audiences from CR) not implemented. Ingress upload service not implemented. |
| [COST-7689](https://redhat.atlassian.net/browse/COST-7689) | Implement RBAC Service | ❌ | RBAC **worker** Deployment (not API — RBAC API is part of the external RBAC service) not yet built. Ticket scope: single worker Deployment wired to external DB and cache. |
| [COST-7690](https://redhat.atlassian.net/browse/COST-7690) | Implement UI and ConsoleLink | ❌ | UI Deployment + OAuth2 Proxy sidecar, ClusterIP Service, ConsoleLink (cluster-scoped, needs finalizer cleanup) not implemented. |
| [COST-7691](https://redhat.atlassian.net/browse/COST-7691) | Implement Routes, NetworkPolicies, and TLS | ❌ | OpenShift Routes (gateway + UI, edge TLS), NetworkPolicies per component, dedicated ServiceAccounts per component, restricted-v2 SCC compliance for all pods, `status.phase → Ready` transition not implemented. CA bundle ConfigMap partially present (init container + service-ca ConfigMap) but not full multi-source merge per ticket. |
| [COST-7692](https://redhat.atlassian.net/browse/COST-7692) | Implement monitoring and alerting | ❌ | Operator metrics endpoint, application ServiceMonitors, PrometheusRules (5 alert rules), and Kubernetes Events on phase transitions/migration/drift not implemented. `monitoring.enabled` flag in CRD only. |

## Lifecycle

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| [COST-7693](https://redhat.atlassian.net/browse/COST-7693) | Implement upgrade and scaling flows | 🔄 | Image-tag-triggered migration re-run ✅, SSA re-applies desired replicas ✅. Missing: automatic rollback to previous image tags on migration failure (`UpgradeFailed` condition + Event), rolling update strategy (maxSurge/maxUnavailable per workload type), profile-based replica scaling. |
| [COST-7694](https://redhat.atlassian.net/browse/COST-7694) | Implement secret rotation and CA management | 🔄 | CA bundle combine init container ✅, service-ca ConfigMap with OCP injection annotation ✅. Missing: rotation trigger via `cost.redhat.com/rotate-secrets` annotation, Django key with correct charset (`!@#$%^&*(-_=+)` etc.), pod template annotation rolling restart, `SecretRotated` Event. |

## OLM & CI

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| [COST-7695](https://redhat.atlassian.net/browse/COST-7695) | Create OLM bundle | 🔄 | `make bundle` target wired, `PROJECT` file and `config/manifests/` kustomize bases present. Bundle not yet generated; CSV not written; `operator-sdk bundle validate` not yet run. |
| [COST-7696](https://redhat.atlassian.net/browse/COST-7696) | Build CI pipeline for bundle | ❌ | `.github/workflows/` scaffolded but not customised. No scorecard tests, no CatalogSource, no OLM install verification. |
| [COST-7697](https://redhat.atlassian.net/browse/COST-7697) | Adapt existing E2E suite for operator | ❌ | Existing pytest suite (88+ tests) not yet adapted. `test/e2e/` is the kubebuilder Go scaffold stub, not the existing test suite. |
| [COST-7698](https://redhat.atlassian.net/browse/COST-7698) | Implement operator-specific E2E scenarios | ❌ | Drift correction, secret rotation, upgrade sequencing, dependency failure, pause/resume E2E tests not written. |
| [COST-7699](https://redhat.atlassian.net/browse/COST-7699) | Set up OpenShift CI integration | ❌ | Prow step, OLM install, external prerequisites provisioning, E2E execution, artifact collection not set up. |
| [COST-7700](https://redhat.atlassian.net/browse/COST-7700) | Write installation and configuration guides | ❌ | Prerequisites guide, Quickstart, Production guide, CMMO configuration guide — none written. README is scaffold placeholder. |

---

## Deviations from Ticket Spec

Items where we intentionally diverge from the JIRA acceptance criteria:

1. **Bundled infrastructure** (COST-7678, COST-7686) — Tickets spec external-only infra (CR takes `host`/`port`/`credentialsSecretRef`). We keep `Deploy: true` options for DB and Cache as an intentional extension — enables dev/PoC deployments without pre-existing infrastructure. External-only mode is also supported; both modes coexist.

Items that need fixing before GA:

2. **Phase names** — Ticket: Discovering/Validating/Migrating/Deploying/Ready. Ours: Provisioning/Running. Should be renamed before any external consumers depend on the status API.

3. **Migration scope** — Ticket: sequential Koku → ROS → RBAC migrate → RBAC seed. We only have Koku. ROS and RBAC migrations are missing.

4. **Django key charset** — Ticket specifies `abcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*(-_=+)`. We use `base64.URLEncoding`. Affects the generated secret value.

---

## Next Priority (per backlog order)

1. **[COST-7678](https://redhat.atlassian.net/browse/COST-7678)** — Restructure CRD: split files, fix phase names, switch infra to external-only, add `DiscoveredConfig` status, add profiles
2. **[COST-7682](https://redhat.atlassian.net/browse/COST-7682)** — Discovery phase (cluster domain, StorageClass)
3. **[COST-7683](https://redhat.atlassian.net/browse/COST-7683)** — S3 auto-detection (OBC → NooBaa → user-provided)
4. **[COST-7684](https://redhat.atlassian.net/browse/COST-7684)** — External dependency validation (TCP + HTTP probes)
5. **[COST-7685](https://redhat.atlassian.net/browse/COST-7685)** — Complete migration: add ROS + RBAC Jobs, fix backoffLimit, add `activeDeadlineSeconds`
6. **[COST-7686](https://redhat.atlassian.net/browse/COST-7686)** — Add ROS API/Processor and Kruize to app services
7. **[COST-7688](https://redhat.atlassian.net/browse/COST-7688)** — Envoy gateway + Ingress upload handler
8. **[COST-7691](https://redhat.atlassian.net/browse/COST-7691)** — Routes + NetworkPolicies + phase→Ready transition
