# Security Context Strategy

Analysis of how the cost management stack should configure pod/container security
contexts on OpenShift, based on four sources of evidence:

1. The on-prem Helm chart (`cost-onprem-chart`) — the reference implementation
2. The koku image Dockerfile — what the image actually requires
3. The JIRA backlog (COST-7691) — the stated acceptance criteria
4. OpenShift operator best practices — the platform standard

---

## Evidence

### 1. Helm chart approach (the reference)

The chart **never sets `runAsUser`** on any Deployment. It uses two reusable
helpers:

**Pod level (`cost-onprem.securityContext.pod.nonRoot`):**
```yaml
runAsNonRoot: true
seccompProfile:
  type: RuntimeDefault
```

**Container level (`cost-onprem.securityContext.container`):**
```yaml
allowPrivilegeEscalation: false
capabilities:
  drop:
    - ALL
```

No explicit UID. No `anyuid` grants. The chart's own install guide notes:
*"Automatically uses restricted-v2 SCC"* — it relies entirely on OpenShift's
SCC admission to inject a UID from the namespace range.

### 2. koku image Dockerfile

```dockerfile
FROM registry.access.redhat.com/ubi9-minimal:latest AS base

ARG USER_ID=1000

RUN adduser koku -u ${USER_ID} -g 0 && \
    chmod ug+rw ${APP_ROOT} ${APP_HOME} ${APP_HOME}/static /tmp

USER koku
```

Key facts:
- Primary group is **0** (root group), not 1000
- All app directories are `ug+rw` — writable by any member of group 0
- This **is** the Red Hat arbitrary-UID convention: files owned by GID 0 with
  group-write permission, so the container functions correctly under any UID
  injected by the namespace SCC

The image is confirmed OpenShift-compatible and works without an explicit
`runAsUser` — exactly as it runs in the SaaS deployment via Clowder, which
also sets no `runAsUser`.

### 3. JIRA specification (COST-7691)

> *"restricted-v2 SCC compliance for all pods"*

The acceptance criterion explicitly requires that every pod pass the
`restricted-v2` SCC without special grants. `restricted-v2` requires:
- `allowPrivilegeEscalation: false`
- `capabilities.drop: [ALL]`
- `runAsNonRoot: true` (enforced by SCC)
- `seccompProfile.type: RuntimeDefault`
- UID **must be in the namespace-allocated range** (injected by the SCC webhook)

Any pod with a hardcoded `runAsUser` outside the namespace range fails this
SCC unless `anyuid` is granted — which contradicts the requirement.

### 4. OpenShift operator best practices

The Operator SDK best-practice guide states:
- Do not hardcode `runAsUser`; let the platform inject it
- Do not grant `anyuid` unless the image unconditionally requires root
- Set `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`,
  and `seccompProfile.type: RuntimeDefault`
- Use `ReadOnlyRootFilesystem: true` where possible; provide `emptyDir`
  mounts for writable paths

---

## Current operator state vs. requirements

| Concern | JIRA requirement | Helm chart | Current operator | Gap |
|---------|-----------------|------------|-----------------|-----|
| `runAsUser` | absent (restricted-v2 injects) | absent | **hardcoded 1000/1001** | ❌ |
| `seccompProfile` | RuntimeDefault | RuntimeDefault (pod level) | **absent** | ❌ |
| `capabilities.drop` | ALL | ALL | ALL on some containers | ⚠️ partial |
| `readOnlyRootFilesystem` | required where possible | Envoy only | init containers + Envoy | ⚠️ partial |
| `anyuid` SCC grants | none (restricted-v2 only) | none | Kruize + ROS SAs | ❌ |
| koku SA anyuid | none | none | none (but RunAsUser: 1000 set) | ❌ inconsistent |

The current operator is internally inconsistent: it sets `RunAsUser: 1000` for
koku pods (requiring anyuid) but never grants anyuid to the koku ServiceAccount.
This worked in previous testing only because the test cluster ran
`oc adm policy add-scc-to-user anyuid` manually.

---

## Root cause of the confusion

An earlier `volumes.go` comment said:

> *"The current koku and ubi-minimal images are not yet built that way — files
> are owned by specific UIDs — so the container would fail at runtime with a
> different injected UID."*

This comment was **incorrect**. The Dockerfile shows `adduser koku -u 1000 -g 0`
with `chmod ug+rw` — the image follows the arbitrary-UID convention.
The comment has been removed.

---

## Recommended solution

Align the operator with the Helm chart and JIRA requirements:

### Container security contexts

**Pod level** (all Deployments and Jobs):
```go
func nonRootPodSC() *corev1.PodSecurityContext {
    nonRoot := true
    return &corev1.PodSecurityContext{
        RunAsNonRoot: &nonRoot,
        SeccompProfile: &corev1.SeccompProfile{
            Type: corev1.SeccompProfileTypeRuntimeDefault,
        },
    }
}
```

**Standard container** (ROS, Envoy, Ingress, init containers):
```go
// allowPrivilegeEscalation: false, capabilities.drop: ALL, readOnlyRootFilesystem: true
// No runAsUser — restricted-v2 injects from namespace range
```

**Koku containers** (API, Masu, Listener, Celery, migration):
```go
// Same as standard BUT readOnlyRootFilesystem: false
// Reason: Django's settings.py unconditionally instantiates a file log
// handler at /opt/koku/koku/app.log regardless of DJANGO_LOG_HANDLERS;
// this causes a startup crash on a read-only root FS.
// Fix: patch koku to respect DJANGO_LOG_HANDLERS at handler init time.
```

### SCC grants

Remove `OpenShiftAnyUIDRoleBinding` for Kruize and ROS ServiceAccounts.
Those images (Kruize: `quay.io/redhat-services-prod/...`, ROS: similar) should
also follow the Red Hat arbitrary-UID convention. If either image fails at
runtime with the injected UID, the fix is in the image, not an anyuid grant.

The only legitimate `anyuid` need is for bundled PostgreSQL and Valkey in
`deploy: true` mode — those are upstream Docker images (`postgres:16`,
`valkey/valkey:8`) that hardcode specific UIDs without group-0 conventions.
This is handled by granting anyuid to the `default` ServiceAccount in the
`deploy-crc.sh` script (dev-only fixture; not part of the operator itself).

### Implementation plan

1. **`volumes.go`** — remove `RunAsUser` from all SC functions; add
   `SeccompProfile: RuntimeDefault` to `nonRootPodSC()`
2. **`controller.go`** — remove the two `OpenShiftAnyUIDRoleBinding` calls for
   Kruize and ROS; remove `scc.go` or keep it as an emergency escape hatch
3. **Test on CRC** — verify Koku migration completes, ROS processor starts,
   Kruize healthchecks pass — all without anyuid grants
4. **Update `deploy-crc.sh`** — document that only `default` SA needs anyuid
   (bundled mode only)

### Risk

The main risk is that Kruize or some ROS image has a non-group-0 USER that
causes runtime failures under an injected UID. Mitigation: run the CRC test
without anyuid grants and observe pod startup logs.

---

## Relationship to COST-7691

This analysis fully covers the "restricted-v2 SCC compliance for all pods"
acceptance criterion. Implementing the solution above would resolve the
remaining SCC gaps in COST-7691 independently of the Routes and
NetworkPolicies items in that ticket.
