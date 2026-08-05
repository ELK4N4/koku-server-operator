# Operator Task Tracker

Tracks implementation status against the COST-7678–7700 Jira backlog.

## Legend
- ✅ Done — implemented and tested on CRC
- 🔄 In Progress — partially implemented
- ❌ Not started

---

## CRD & API

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| COST-7678 | Define CostManagementCRD types | ✅ | Full typed spec in `api/v1alpha1/costmanagementserviceconfig_types.go`. All 15 top-level sections. |
| COST-7679 | Sample CRs and generate manifests | ✅ | `config/samples/costmanagement-service-cfg_v1alpha1_costmanagementserviceconfig.yaml`. CRD YAML generated via `make manifests`. |

## Reconciler Core

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| COST-7680 | Phase-gated reconciler skeleton | ✅ | 6-stage pipeline with `runPhases()` + `PhaseError`. Stages: shared-config → infra → migration-gate → core → workers → edge. |
| COST-7681 | Server-Side Apply and ownership model | ✅ | `apply()` uses `client.Apply + ForceOwnership`. StatefulSet handled separately (VCT immutability). Secrets use create-only `ensureSecret()`. |
| COST-7682 | Cluster discovery | ❌ | S3 endpoint, storage class, cluster domain auto-detection not yet implemented. Currently requires explicit values in CR. |
| COST-7683 | S3 backend auto-detection | ❌ | Placeholder storage credentials secret created. Real ODF/NooBaa/OBC detection not implemented. |
| COST-7684 | External dependency validation | ❌ | No preflight check for Kafka, S3, external DB/cache connectivity. |

## Infrastructure

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| COST-7685 | Migration Job lifecycle | ✅ | Job created pre-deploy, upgrade-detection by image tag, completion/failure gating, `Result{Stop:true}` on failure. |

## Application Services

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| COST-7686 | Application services | ✅ | PostgreSQL, Valkey, Koku API, Masu, Listener all `1/1 Running` on CRC (arm64). Koku API serving `/livez` + `/readyz`. |
| COST-7687 | Workers and scheduled jobs | 🔄 | Celery beat + 5 active workers (default, priority, summary×2, ocp×2) `1/1 Running`. 5 disabled workers at 0 replicas. ROS, RBAC, Kruize, Ingress builders still TODO. |
| COST-7688 | Gateway and Ingress | ❌ | Ingress upload handler stub only. |
| COST-7689 | RBAC Service | ❌ | RBAC API + worker Deployments not yet implemented (stage 5 stub). |
| COST-7690 | UI and ConsoleLink | ❌ | Stage 6 stub. |
| COST-7691 | Routes, NetworkPolicies, TLS | ❌ | Stage 6 stub. |
| COST-7692 | Monitoring and alerting | ❌ | ServiceMonitor not yet wired. `monitoring.enabled` flag in CRD only. |

## Lifecycle

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| COST-7693 | Upgrade and scaling flows | 🔄 | Image-tag-triggered migration re-run implemented. Ordered rollout via stage gates. Scaling via replicas field. No canary/blue-green. |
| COST-7694 | Secret rotation and CA management | 🔄 | DB + Django secrets are create-only (no rotation). CA bundle combine init container present. Service CA ConfigMap with OCP injection annotation. |

## OLM & CI

| Ticket | Summary | Status | Notes |
|--------|---------|--------|-------|
| COST-7695 | OLM bundle | 🔄 | `make bundle` target wired. `PROJECT` file and `config/manifests/` present. Bundle not yet generated or validated. |
| COST-7696 | CI pipeline for bundle | ❌ | `.github/workflows/` scaffolded but not customised. |
| COST-7697 | Adapt existing E2E suite | ❌ | |
| COST-7698 | Operator-specific E2E scenarios | ❌ | `test/e2e/` scaffold only. |
| COST-7699 | OpenShift CI integration | ❌ | |
| COST-7700 | Installation and configuration guides | ❌ | README is the scaffold placeholder. |

---

## Known Issues / Bugs

| Issue | File | Fix |
|-------|------|-----|
| Koku containers crash on read-only FS | `resources/volumes.go` | `kokuAppContainerSC()` removes `ReadOnlyRootFilesystem` — Django instantiates all logging handlers unconditionally |
| `DJANGO_LOG_HANDLERS=console` insufficient | `resources/env.go` | Env var is set but doesn't prevent file handler instantiation at Django startup |
| CRC arm64 / koku image | `config/samples/` | Sample CR uses `quay.io/martin_povolny/koku:latest` (arm64). Production tag: `quay.io/redhat-services-prod/cost-mgmt-dev-tenant/koku:d8055ac` (amd64) |
| `ComponentStatus.Ready` omitempty | `api/v1alpha1/` | Boolean zero value omitted by merge patch; `+optional` + omitempty fixes CRD validation |
| Stale `Degraded` condition | controller | Old failed-migration condition not cleared on success |

---

## Next Priority

1. **COST-7686 completion** — Celery readiness gate, ROS, RBAC, Kruize resource builders
2. **COST-7688** — Ingress upload handler
3. **COST-7691** — OpenShift Route for the gateway
4. **COST-7682/7683** — Cluster + S3 auto-detection (highest user-facing value)
5. **COST-7695** — OLM bundle generation and validation
