# CRD Naming Audit: koku-service-operator

**Date:** 2026-06-17
**Author:** Pablo (with AI-assisted research)
**Audience:** Martin, Cost Management team
**Status:** Pre-GA — early enough to re-scaffold without migration cost

---

## Executive Summary

The current CRD configuration (`cost.redhat.com / CostManagement`) places downstream product branding in the upstream repository. This conflicts with Operator Framework best practices, Kubernetes API conventions, and established Red Hat upstream/downstream patterns. We recommend changing the domain and group before any release.

---

## Current Configuration

From the [`PROJECT`](https://github.com/project-koku/koku-service-operator/blob/main/PROJECT) file:

| Field | Current Value |
|-------|--------------|
| Domain | `redhat.com` |
| Group | `cost` |
| Full API group | `cost.redhat.com` |
| Version | `v1alpha1` |
| Kind | `CostManagement` |
| CRD name | `costmanagements.cost.redhat.com` |
| apiVersion in CR | `cost.redhat.com/v1alpha1` |

For comparison, the existing koku-metrics-operator:

| Field | koku-metrics-operator |
|-------|----------------------|
| Domain | `openshift.io` |
| Group | `costmanagement-metrics-cfg` |
| Full API group | `costmanagement-metrics-cfg.openshift.io` |
| Kind | `CostManagementMetricsConfig` |

---

## Issues Identified

### 1. `redhat.com` domain in an upstream repository

**Severity: High**

The Kubernetes [API conventions](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-architecture/api-conventions.md) state:

> "When choosing a group name, we recommend selecting a subdomain your group or organization owns, such as `widget.mycompany.com`."

The upstream repo lives at `project-koku/koku-service-operator`. Using `redhat.com` signals corporate ownership of the API surface, which:

- Discourages external contributors (they'd be authoring CRDs under Red Hat's domain)
- Has no precedent among successful Red Hat upstream projects (see industry survey below)
- Conflates the upstream community project with the downstream product

### 2. Group `cost` is too generic

**Severity: Medium**

The group `cost` could collide with other FinOps operators (OpenCost, Kubecost, any future `cost.*` CRD). The Kubernetes community recommends groups specific enough to avoid collision. Our own koku-metrics-operator uses `costmanagement-metrics-cfg` precisely to avoid this.

### 3. Kind `CostManagement` is downstream product naming

**Severity: Low-Medium**

`CostManagement` is the Red Hat product name. Using it as the Kind in the upstream repo muddies the boundary between community and product. This matters for community perception but is less critical than the domain issue.

---

## Industry Survey: How Red Hat Upstream Projects Handle This

### Projects that use a project-owned domain (strongest community signal)

| Upstream Project | API Group | Downstream Product | Same CRD? |
|-----------------|-----------|-------------------|-----------|
| Strimzi | `kafka.strimzi.io` | AMQ Streams / Streams for Apache Kafka | Yes |
| Crunchy PGO | `postgres-operator.crunchydata.com` | Red Hat OpenShift Database Access | Yes |
| Keycloak | `k8s.keycloak.org` | Red Hat build of Keycloak | Yes |

### Projects that use `openshift.io` (OpenShift-ecosystem community domain)

| Upstream Project | API Group | Notes |
|-----------------|-----------|-------|
| koku-metrics-operator | `costmanagement-metrics-cfg.openshift.io` | Our own companion operator |
| Cluster Ingress Operator | `operator.openshift.io` | OpenShift core |
| Image Registry Operator | `imageregistry.operator.openshift.io` | OpenShift core |
| Samples Operator | `samples.operator.openshift.io` | OpenShift core |

### Projects that use `redhat.com` in upstream repos

**None found.** The `redhat.com` domain appears only in downstream-only contexts (Red Hat certified catalog metadata, product documentation) or in repos already under the `openshift/` org that are inherently downstream.

---

## Operator Framework Best Practices Checklist

| Best Practice | Current | Compliant? |
|--------------|---------|-----------|
| Domain is owned by or represents the project | `redhat.com` (corp) | No |
| Group is specific enough to avoid collision | `cost` (generic) | No |
| Kind is PascalCase and singular | `CostManagement` | Yes |
| CRD name follows `<plural>.<group>` format | `costmanagements.cost.redhat.com` | Yes |
| Version uses standard maturity prefix | `v1alpha1` | Yes |
| Scope is appropriate (Namespaced for per-tenant) | `Namespaced` | Yes |

---

## Recommended Options

### Option A: `openshift.io` domain (recommended — pragmatic choice)

Best for: Consistency with existing koku-metrics-operator, OpenShift-only scope.

```yaml
# PROJECT file
domain: openshift.io
group: koku-server
kind: KokuServer
version: v1alpha1
```

| Field | Value |
|-------|-------|
| Full API group | `koku-server.openshift.io` |
| CRD name | `kokuservers.koku-server.openshift.io` |
| apiVersion in CR | `koku-server.openshift.io/v1alpha1` |

**Pros:**
- Consistent with `costmanagement-metrics-cfg.openshift.io`
- `openshift.io` is the established community domain for OpenShift-native operators
- Zero collision risk
- Clearly community-owned

**Cons:**
- Ties the project to OpenShift branding (acceptable since we're OpenShift-only)

**Scaffold command:**
```bash
operator-sdk init --domain openshift.io --repo github.com/project-koku/koku-service-operator
operator-sdk create api --group koku-server --version v1alpha1 --kind KokuServer --resource --controller
```

### Option B: Project-owned domain (strongest community signal)

Best for: If we envision Koku growing independent community identity (Strimzi model).

```yaml
# PROJECT file
domain: koku.io
group: server
kind: KokuServer
version: v1alpha1
```

| Field | Value |
|-------|-------|
| Full API group | `server.koku.io` |
| CRD name | `kokuservers.server.koku.io` |
| apiVersion in CR | `server.koku.io/v1alpha1` |

**Pros:**
- Strongest community signal (project owns its identity)
- Platform-neutral (works if Koku ever expands beyond OpenShift)
- Follows Strimzi/Keycloak/Crunchy pattern exactly

**Cons:**
- Requires owning/registering a domain
- Different domain family from koku-metrics-operator

---

## How Downstream Branding Works (Either Option)

Following the Strimzi/AMQ Streams pattern, the downstream operator (`cost-management-server-operator`) shares the **same CRD**. What changes:

| Aspect | Upstream | Downstream |
|--------|----------|------------|
| Operator name | `koku-service-operator` | `cost-management-server-operator` |
| CSV displayName | "Koku Server Operator" | "Cost Management Server Operator" |
| Container images | `quay.io/project-koku/...` | `registry.redhat.io/cost-management/...` |
| OLM catalog | `community-operators` | `redhat-operators` |
| CRD / API group | **Same** | **Same** |
| Kind | **Same** | **Same** |
| Support level | Community | Red Hat subscription |

Users write the same CR YAML regardless of whether they installed from the community or Red Hat catalog. This simplifies documentation, migrations, and community contributions.

---

## Migration Risk Assessment

| Factor | Assessment |
|--------|-----------|
| Current commit count | 4 (scaffolding only) |
| Published releases | None |
| Users with existing CRs | None |
| Migration complexity | Trivial (re-scaffold) |
| Cost of waiting | Increases with every release — CRD API group changes post-GA require conversion webhooks and user migration |

**Conclusion:** Now is the time to fix this. After GA, changing the API group becomes a breaking change requiring a deprecation cycle.

---

## Decision Needed

- [ ] **Option A** (`koku-server.openshift.io`) — pragmatic, consistent with existing operator
- [ ] **Option B** (`server.koku.io`) — strongest community identity signal
- [ ] **Keep current** (`cost.redhat.com`) — requires justification against above findings

---

## References

- [Kubernetes API Conventions — Group Names](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-architecture/api-conventions.md)
- [Operator SDK — Go Tutorial (domain usage)](https://master.sdk.operatorframework.io/docs/building-operators/golang/tutorial/)
- [Strimzi / AMQ Streams CRD sharing](https://github.com/strimzi/strimzi-kafka-operator/issues/1794)
- [Crunchy PGO CRD Reference](https://access.crunchydata.com/documentation/postgres-operator/5.1.1/references/crd/)
- [Keycloak Operator Migration (domain change case study)](https://docs.redhat.com/en/documentation/red_hat_build_of_keycloak/26.6/html/migration_guide/migrating-operator)
- [OpenShift Operator APIs (openshift.io usage)](https://docs.redhat.com/en/documentation/openshift_container_platform/4.16/observability/operator_apis/openshiftapiserver-operator-openshift-io-v1)
- [koku-metrics-operator PROJECT file](https://github.com/project-koku/koku-metrics-operator/blob/main/PROJECT)
