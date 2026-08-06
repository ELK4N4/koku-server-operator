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

## 2. Bundled Infrastructure (Dev/Test Convenience Only)

### What the JIRAs specify (COST-7678, COST-7686)

All infrastructure is external-only. The CR accepts connection details and a
`credentialsSecretRef` for each dependency (PostgreSQL, Redis/Valkey, Kafka,
S3, OIDC). The operator does not provision any infrastructure. **The JIRAs
are correct for the production target.** In production on-premise deployments,
PostgreSQL, Kafka, and object storage are the customer's responsibility —
typically operated by dedicated teams or products (CNPG, AMQ Streams, ODF).

### What we implement

The CRD has a `deploy: true` option for database and cache:

```yaml
database:
  deploy: true   # TESTING ONLY — provisions a bare PostgreSQL StatefulSet
  # OR
  deploy: false  # PRODUCTION — connects to an external instance
  host: "postgres.databases.svc.cluster.local"
  secretName: "my-db-credentials"
```

### Scope and limitations of `deploy: true`

The bundled mode is a **developer and CI convenience**. It exists solely to
allow running the full stack without pre-provisioning external services, which
is useful for:

- Local development (`make run` against CRC)
- Integration tests that need a self-contained environment
- Demonstrating the operator without a full infrastructure stack

It is **explicitly out of scope for production**:

- No high availability (single-replica StatefulSet, no replication)
- No backup, point-in-time recovery, or day-2 operations
- No connection pooling, monitoring integration, or certificate management
- Storage class and sizing are minimal defaults
- Kafka cannot be bundled at all (AMQ Streams is always external)

The production CRD path — `deploy: false` with all six dependency types
wired via BYOI connection details and `credentialsSecretRef` — is the primary
design target and matches the JIRA specification exactly.

### Implication for design decisions

Any feature, reconciler stage, or status condition that interacts with the
database, cache, or object storage should be designed around the **external
(BYOI) path**. The bundled path must not drive API shape or reconciler
complexity. If the bundled path requires special-casing in the reconciler,
that is a sign the abstraction needs to be reconsidered.

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

### What we implement and why

A single `costmanagementserviceconfig_types.go`. **The file split is
intentionally skipped.**

In Go, files within a package are compiled identically — splitting
`infra_types.go` from `app_types.go` has no semantic effect. Types are found
and accessed the same way by the compiler, by controller-gen, and by IDE
tooling regardless of which file they live in.

The split has marginal organisational value at the current file size (~500
lines). It becomes worthwhile if the file grows significantly or if multiple
developers are editing different sections simultaneously and producing merge
conflicts. Neither condition applies now.

**What is worth isolating** (and will be, when implemented):

- `defaults.go` and `validation.go` — these are admission webhook handlers,
  a genuinely different concern from type definitions. They will be created
  as separate files when the webhooks are implemented (COST-7678 backlog).
- `status_types.go` — could be split if the status section grows
  significantly, but not a priority.

The remaining spec-type splits (`infra_types.go`, `app_types.go`,
`profiles.go`) are cosmetic and will only be done if the file becomes hard
to navigate.

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

---

## 6. Additional Best Practice Issues (discovered by code audit)

These were not covered by the JIRA tickets but found during implementation review.

### 6a. `bool` + `omitempty` + `+kubebuilder:default:=true` — cannot opt out

Twelve fields in the types use this combination:

```go
// +kubebuilder:default:=true
Deploy bool `json:"deploy,omitempty"`
```

Because `false` is the Go zero value, `omitempty` drops it during marshaling.
Any client (SDK, test, admission webhook) that constructs the struct in Go and
marshals it will silently lose an explicit `false`, causing CRD defaulting to
re-apply `true`. This makes it impossible to opt out of `Deploy`, `UseSSL`,
`ScheduleReportChecks`, etc. through code.

**Fix:** Use `*bool` for all fields with `+kubebuilder:default:=true`:

```go
// +kubebuilder:default:=true
Deploy *bool `json:"deploy,omitempty"`
```

`nil` means "use the default"; `false` is an explicit opt-out that survives
marshaling.

### 6b. `ComponentStatus` is a homebrew condition type

```go
type ComponentStatus struct {
    Ready   bool   `json:"ready,omitempty"`
    Message string `json:"message,omitempty"`
}
```

This runs in parallel with the `metav1.Condition` slice in `Status.Conditions`.
It lacks `LastTransitionTime`, `Reason` (machine-readable), and
`ObservedGeneration`, making `kubectl wait --for=condition=DatabaseReady` and
standard condition-watching tooling unusable for individual components.

**Fix:** Replace `ComponentStatuses` with additional entries in the top-level
`Status.Conditions` slice using `metav1.Condition` with typed `type` strings
(`DatabaseReady`, `CacheReady`, `SchemaUpToDate`, etc.). This is also what
COST-7678 specifies in its condition list.

### 6c. Passwords in the CR spec (`RealmUsers`)

```go
type RealmUser struct {
    Password string `json:"password"`
    ...
}
```

`RealmUsers` stores Keycloak user passwords directly in the CR spec. CR specs
are stored in etcd, appear in `kubectl get -o yaml`, and are captured in audit
logs. This applies even if the intent is "initial/bootstrap only".

**Fix:** Remove `Password` from `RealmUser`. Keycloak user provisioning should
read credentials from a referenced Secret, or be delegated entirely to a
separate Job that reads from a Secret outside the CR.

### 6d. `Env map[string]string` raw override leaks into CR spec

`KokuAPISpec.Env`, `MasuSpec.Env`, and `ListenerSpec.Env` allow arbitrary
environment variables to be placed in the CR. This means sensitive values
(`DATABASE_PASSWORD`, `DJANGO_SECRET_KEY`) can be stored in etcd unencrypted
via the CR. Kubernetes Secrets exist precisely to avoid this.

**Fix:** Expose only specific, typed fields for known configuration points.
If an escape hatch is genuinely needed for non-sensitive tuning knobs, rename
to `additionalEnv` and document that it must not contain secrets. Validate
via a webhook that known sensitive key names are rejected.

### 6e. `OwnerReference` missing `Controller` and `BlockOwnerDeletion`

`setOwnerRef` sets only `APIVersion`, `Kind`, `Name`, `UID`. Without
`Controller: true` the garbage collector cannot resolve ownership conflicts.
Without `BlockOwnerDeletion: true` foreground deletion of the CR does not
wait for child resources to be removed first, leaving dangling resources in a
terminating namespace.

**Fix:**

```go
ref := metav1.OwnerReference{
    APIVersion:         ...,
    Kind:               "CostManagementServiceConfig",
    Name:               owner.Name,
    UID:                owner.UID,
    Controller:         boolPtr(true),
    BlockOwnerDeletion: boolPtr(true),
}
```

### 6f. No finalizer for cluster-scoped resources

`setOwnerRef` explicitly skips cluster-scoped objects (comment: "owner refs
don't apply"). When Kruize ClusterRole/ClusterRoleBinding and the UI
ConsoleLink are created (COST-7686, COST-7690), there is no finalizer to
ensure they are removed when the CR is deleted.

**Fix:** Register a finalizer (`cost.redhat.com/cleanup`) on the CR before
creating any cluster-scoped resource. In the deletion path, remove the
cluster-scoped resources, then remove the finalizer. This is standard
operator practice and is required by COST-7681.

### 6g. `StatefulSet` update bypasses SSA

All resources use Server-Side Apply via `r.Patch(…, client.Apply, …)` except
the PostgreSQL StatefulSet, which uses `r.Update` and patches only four
fields. This means changes to init containers, volumes, security context, or
labels are silently ignored on subsequent reconciliations. The StatefulSet
diverges from desired state and no tool can detect it (SSA tracks field
ownership; `r.Update` does not).

The workaround is necessary today because StatefulSet `VolumeClaimTemplates`
are immutable. The correct approach is to apply only the mutable portion of
the spec via SSA (excluding `VolumeClaimTemplates`) and detect VCT changes
separately, surfacing them as a condition requiring manual intervention.

### 6h. No periodic requeue on clean reconcile

`reconcile` returns `ctrl.Result{}` (no `RequeueAfter`) on success, so
drift in managed resources is only corrected when a watch event fires. For
cluster-scoped resources (not owned, not watched) external drift is never
detected. COST-7681 requires a 5-minute periodic requeue.

**Fix:** Return `ctrl.Result{RequeueAfter: 5 * time.Minute}` at the end of
a successful reconcile pass.

### 6i. `MergeEnv` appends without deduplication

`MergeEnv` in `env.go` appends override keys without checking whether they
already exist in the base set, producing duplicate env var entries in pod
specs. Kubernetes honours the last occurrence but the earlier one wastes
memory and confuses tooling. The override should replace the earlier entry.

---

## Summary

| Topic | JIRA spec | Our implementation | Intentional? |
|-------|-----------|--------------------|-------------|
| Status primary API | Phase enum (linear) | Conditions (composable) + Phase as convenience | ✅ Yes — Kubernetes best practices |
| Phase names | Discovering/Validating/Migrating/Deploying/Ready | Provisioning/Running (to rename) | 🔄 Rename pending |
| Bundled infra | External-only (correct for production) | `deploy: true` exists for dev/CI only; production path is BYOI | ⚠️ Testing convenience — not a production feature |
| CRD file split | 6 files + webhooks | Single file | 🔄 Refactor pending (COST-7678) |
| Migration scope | Koku + ROS + RBAC | Koku only | ❌ Gap (COST-7685) |
| Migration `backoffLimit` | 3 | 0 | ❌ Gap |
| Django key charset | `a-z0-9!@#$%^&*(-_=+)` | base64 URL encoding | ❌ Gap (COST-7694) |
| `bool` + `omitempty` + default:true | — | 12 fields affected | ❌ Bug (§6a) |
| `ComponentStatus` homebrew type | use `metav1.Condition` | parallel homebrew struct | ❌ Gap (§6b) |
| Passwords in CR spec | — | `RealmUser.Password` | ❌ Security issue (§6c) |
| `Env map[string]string` | — | unvalidated override in spec | ❌ Security risk (§6d) |
| `OwnerReference` fields | Controller+BlockOwnerDeletion | neither set | ❌ Bug (§6e) |
| Finalizer for cluster-scoped | required (COST-7681) | not implemented | ❌ Gap (§6f) |
| StatefulSet update path | SSA | `r.Update` partial patch | ❌ Gap (§6g) |
| Periodic requeue | 5 min (COST-7681) | none | ❌ Gap (§6h) |
| `MergeEnv` deduplication | — | appends, no dedup | ❌ Bug (§6i) |

[k8s-conventions]: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
