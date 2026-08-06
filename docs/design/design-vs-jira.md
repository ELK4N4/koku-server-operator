# Design Decisions vs JIRA Specification

This document explains where and why the operator implementation intentionally
diverges from the JIRA backlog (COST-7678–7700). The source tickets are in
`docs/jira/`. Where we differ, the reasoning is grounded in Kubernetes API
conventions and operator best practices — not in disagreement with the goal of
the ticket.

---

## 1. Status API: Conditions over Phase enum

### What the JIRAs specify (COST-7678, COST-7680)

A `Phase` field with a linear enum mirroring the internal reconciler pipeline:

```
Pending → Discovering → Validating → Migrating → Deploying → Ready → Degraded
```

Each phase corresponds to one reconciler stage. The phase advances
sequentially; only one phase is visible at a time.

### What we implement

The [Kubernetes API conventions][k8s-conventions] explicitly state:

> *"Think twice before adding a phase field. Conditions are generally a better
> replacement."*

The OpenShift operator standard (CVO, cluster-logging-operator, cert-manager)
uses **three top-level conditions** as the primary machine-readable API:

| Condition | Meaning |
|-----------|---------|
| `Available` | core functionality is working and the CR is serving its purpose |
| `Progressing` | the operator is actively working toward the desired state |
| `Degraded` | the operator has failed and cannot make progress without intervention |

Below these, **component-specific conditions** carry the detail:
`DatabaseReady`, `CacheReady`, `KafkaReady`, `StorageReady`,
`AuthenticationReady`, `SchemaUpToDate`.

We keep a `Phase` field — **Pending / Provisioning / Running / Degraded** —
but as a human-readable convenience for `kubectl get cmsc`, not as the primary
API. It is derived from the conditions, not the other way around.

### Why conditions are better than a linear phase enum

1. **Non-linear failures.** If the database is ready but S3 fails, the phase
   can only show one thing. Conditions are independent: `DatabaseReady: True`,
   `StorageReady: False`. External tools (monitoring, scripts) can react to
   each independently.

2. **Machine-queryability.** `--field-selector status.conditions[?type=Available].status=True`
   is a stable API contract. A phase enum value has no such guarantee.

3. **Activity vs outcome.** `Migrating` tells you what the operator is doing.
   `SchemaUpToDate: False` tells you what is wrong from the user's perspective.
   Kubernetes consumers care about the latter.

4. **Backward compatibility.** Renaming or reordering phase enum values is a
   breaking change. Adding a new condition type is not.

### Practical reconciliation

The five JIRA reconciler stages (Discovery, Validation, Migrations, Application,
Platform) remain as internal implementation stages. They are surfaced via the
`Progressing` condition's `reason` and `message` fields:

```yaml
status:
  conditions:
    - type: Progressing
      status: "True"
      reason: RunningMigrations
      message: "Running Koku schema migration job (1 of 3)"
    - type: DatabaseReady
      status: "True"
    - type: StorageReady
      status: "False"
      reason: OBCNotFound
      message: "No ObjectBucketClaim found in namespace cost-onprem"
```

Users see a rich, composable status. The internal pipeline is an
implementation detail.

---

## 2. Bundled Infrastructure

### What the JIRAs specify (COST-7678, COST-7686)

All infrastructure is external-only. The CR accepts connection details and a
`credentialsSecretRef` for each dependency (PostgreSQL, Redis/Valkey, Kafka,
S3, OIDC). The operator does not provision any infrastructure.

### What we implement

Both modes coexist in the CRD:

```yaml
database:
  deploy: true   # bundled mode: operator provisions a PostgreSQL StatefulSet
  # OR
  deploy: false  # BYOI mode: operator connects to an external instance
  host: "postgres.databases.svc.cluster.local"
  secretName: "my-db-credentials"
```

The same pattern applies to the cache (Valkey).

### Why we keep the bundled option

The JIRA scope targets production on-premise deployments where customers
already operate PostgreSQL and Kafka. For that audience, external-only is
correct.

However, the operator also needs to be usable for:

- **Development and CI** — a developer running the stack locally does not want
  to provision a separate database cluster
- **Proof-of-concept deployments** — a customer evaluating the product should
  be able to apply one CR and have a working system
- **Integration testing** — the test suite needs a self-contained environment

The bundled option costs nothing architecturally (it is additive, not a
replacement) and removes a significant adoption barrier. The BYOI path is
fully implemented and tested; `deploy: true` is an intentional extension
beyond the ticket scope.

---

## 3. CRD File Structure

### What the JIRAs specify (COST-7678)

The types split across six files:

```
api/v1alpha1/
  costmanagement_types.go   # top-level CR
  infra_types.go            # DatabaseSpec, CacheSpec, KafkaSpec, ...
  app_types.go              # AppSpec, per-component overrides
  status_types.go           # Phase, conditions, ComponentStatus, DiscoveredConfig
  profiles.go               # standard / ha profile enum and resource sizing maps
  defaults.go               # mutating webhook (defaulting)
  validation.go             # validating webhook + CEL rules
```

### What we implement

Currently a single `costmanagementserviceconfig_types.go`. The split is
correct and is planned for COST-7678; it has not yet been done because the
file structure is a refactor with no behavioral change, and other tickets
(COST-7682 Discovery, COST-7684 Validation) add new fields that will land in
those files anyway — splitting first and then immediately adding to the new
files is the right order.

The webhooks (`defaults.go`, `validation.go`) will be implemented alongside
the split.

---

## 4. Migration Scope

### What the JIRAs specify (COST-7685)

Three migration Jobs run sequentially:

1. Koku schema migration (`manage.py migrate`)
2. ROS schema migration
3. RBAC schema migration + data seeding (bootstrap roles/permissions)

Each has `backoffLimit: 3` and `activeDeadlineSeconds: 600`. Previously
succeeded Jobs are not re-run.

### What we implement

Only the Koku migration Job is implemented. ROS and RBAC Jobs are missing.
`backoffLimit` is currently 0 (we re-create the Job on failure rather than
using Kubernetes retry). `activeDeadlineSeconds` is not set.

This is a known gap, not a design decision. It will be fixed in the COST-7685
completion work.

---

## 5. Django Secret Key Charset

### What the JIRAs specify (COST-7678, COST-7686)

The Django secret key must use `crypto/rand` with the charset:

```
abcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*(-_=+)
```

This matches the character set Django itself uses when generating keys, which
matters for entropy distribution over the allowed character set.

### What we implement

`base64.URLEncoding` over random bytes. The entropy is equivalent (both are
~50 bytes of randomness), but the character set differs — `base64.URLEncoding`
uses `A-Za-z0-9-_` plus `=` padding. Django does not validate the character
set of the key at runtime, so this works, but it does not match the spec.

This will be fixed in the COST-7678 / COST-7694 rotation work.

---

## Summary

| Topic | JIRA spec | Our implementation | Intentional? |
|-------|-----------|--------------------|-------------|
| Status primary API | Phase enum (linear) | Conditions (composable) + Phase as convenience | ✅ Yes — Kubernetes best practices |
| Phase names | Discovering/Validating/Migrating/Deploying/Ready | Provisioning/Running (to rename) | 🔄 Partial — rename pending |
| Bundled infra | External-only | External + bundled both supported | ✅ Yes — dev/PoC use case |
| CRD file split | 6 files + webhooks | Single file | 🔄 Refactor pending (COST-7678) |
| Migration scope | Koku + ROS + RBAC | Koku only | ❌ Gap (COST-7685) |
| Migration `backoffLimit` | 3 (Kubernetes retry) | 0 (operator re-creates) | ❌ Gap |
| Django key charset | `a-z0-9!@#$%^&*(-_=+)` | base64 URL encoding | ❌ Gap (COST-7694) |

[k8s-conventions]: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
