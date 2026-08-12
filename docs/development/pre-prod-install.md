# Pre-production install guide (BYOI → operator → UI)

End-to-end path for a **pre-prod / lab** OpenShift cluster: optional fixture
dependencies (BYOI), then the mandatory operator + `CostManagementServiceConfig`,
ending at a working Cost Management UI login.

This is **not** an OLM Catalog / production packaging guide (COST-7695). It uses
the OwnNamespace model: **operator install namespace == CR namespace**. BYOI
infra may live elsewhere and is referenced only via CR connection fields.
See [ownnamespace.md](ownnamespace.md).

## Conventions (parameterized)

The worked example uses the checked-in BYOI sample names. Override as needed
(chart pytest often uses `cost-tests` / `cost-onprem`).

| Variable | Sample default | Meaning |
|----------|----------------|---------|
| `NAMESPACE` | `cost-byoi` | CR **and** operator namespace |
| `CR_NAME` | `cost-management` | `CostManagementServiceConfig` metadata.name |
| Infra NS | `cost-byoi-infra` | Postgres, Valkey, MinIO |
| Kafka NS | `kafka` | AMQ Streams cluster |
| Keycloak NS | `keycloak` | RHBK (external; never owned by this operator) |

```bash
export NAMESPACE=cost-byoi
export CR_NAME=cost-management
# Chart-style alternative:
# export NAMESPACE=cost-tests CR_NAME=cost-onprem
```

UI Route host pattern:

`https://${CR_NAME}-ui-${NAMESPACE}.<apps-domain>/`

## Prerequisites

- OpenShift cluster with `oc` / `kubectl` and cluster-admin (or equivalent)
- Default StorageClass
- Ability to pull Red Hat registry images (`registry.redhat.io/…`) via the
  cluster pull secret
- A place to push an **linux/amd64** operator image (typical OCP nodes are amd64;
  Apple Silicon CRC is the opposite case — see [crc-testing.md](crc-testing.md))

Clone this repo and work from the root.

---

## Part A — Optional dependencies (BYOI)

Skip any subsection if you already have equivalent customer-managed services.
Point the CR at those endpoints instead of the fixture hostnames.

### A1. Kafka (AMQ Streams) — recommended

```bash
STORAGE_CLASS=<your-sc> LOG_LEVEL=INFO ./config/samples/byoi/deploy-kafka.sh
# Bootstrap (default):
#   cost-onprem-kafka-kafka-bootstrap.kafka.svc:9092
```

Lightweight Redpanda alternative: see [config/samples/byoi/README.md](../../config/samples/byoi/README.md).

### A2. Postgres, Valkey, MinIO

```bash
oc adm policy add-scc-to-user anyuid -z byoi-infra -n cost-byoi-infra 2>/dev/null || true
kubectl apply -k config/samples/byoi/infra

kubectl -n cost-byoi-infra rollout status deploy/postgresql --timeout=180s
kubectl -n cost-byoi-infra rollout status deploy/valkey --timeout=120s
kubectl -n cost-byoi-infra rollout status deploy/minio --timeout=120s
kubectl -n cost-byoi-infra wait --for=condition=complete job/minio-init --timeout=120s
```

Credentials in these YAMLs are **fixed test values — not for production**.

### A3. Keycloak / RHBK (required for UI login)

The operator never deploys Keycloak. Use the chart script (sibling repo) or any
OIDC provider that can issue the UI client.

From the **cost-onprem-chart** checkout:

```bash
# Align redirect URI construction with this install:
export COST_MGMT_NAMESPACE="$NAMESPACE"
export COST_MGMT_RELEASE_NAME="$CR_NAME"
# Optional explicit override:
# export COST_MGMT_UI_BASE_URL="https://${CR_NAME}-ui-${NAMESPACE}.apps.example.com"

LOG_LEVEL=INFO ./scripts/deploy-rhbk.sh
```

The script creates realm user **`admin` / `admin`** by default and writes Secret
`keycloak-client-secret-cost-management-ui` in the Keycloak namespace.

If Keycloak was installed before the UI Route existed, re-run with
`COST_MGMT_UI_BASE_URL` set (or patch the UI client redirect URI to
`https://<ui-host>/oauth2/callback`).

### A4. Mirror UI OAuth client Secret into the CR namespace

```bash
kubectl get ns "$NAMESPACE" >/dev/null 2>&1 || kubectl create ns "$NAMESPACE"

NAMESPACE="$NAMESPACE" CR_NAME="$CR_NAME" \
  ./config/samples/byoi/mirror-ui-oauth-secret.sh
# Creates Secret ${CR_NAME}-ui-oauth-client (keys: client-id, client-secret)
```

Without this Secret, condition `UIReady` stays False and the UI Deployment is
not applied.

---

## Part B — Mandatory: operator + CR

### B1. Build and push an amd64 operator image

```bash
export IMG=quay.io/<your-org>/koku-service-operator:preprod
docker buildx build --platform linux/amd64 -t "$IMG" --push .
```

### B2. Install CRDs, OwnNamespace RBAC, and run the operator in-cluster

`make run` / out-of-cluster controllers **cannot** resolve `*.svc.cluster.local`
BYOI hosts from a laptop. Use the in-cluster helper (binds `default` SA in
`$NAMESPACE`, same as `./hack/deploy-crc.sh`):

```bash
IMG="$IMG" ./hack/deploy-incluster.sh "$NAMESPACE"
```

That script:

1. Applies CRDs + `manager-role` / `manager-cluster-role`
2. Creates RoleBinding + ClusterRoleBinding for `$NAMESPACE:default`
3. Deploys `koku-service-operator` in `$NAMESPACE` with `NAMESPACE` from the pod

Watch logs:

```bash
kubectl -n "$NAMESPACE" logs -f deploy/koku-service-operator
```

### B3. App Secrets, then the CR

```bash
# Sample secrets assume namespace cost-byoi — edit metadata.namespace if needed.
kubectl apply -f config/samples/byoi/app/secrets.yaml

# Edit before apply:
#   - metadata.namespace / metadata.name  → $NAMESPACE / $CR_NAME
#   - spec.global.clusterDomain           → apps.<your-cluster>
#   - spec.auth.keycloak.issuerURL        → public Keycloak issuer (iss)
#   - spec.auth.keycloak.tls              → caCertSecretName or insecureSkipVerify (dev)
kubectl apply -f config/samples/byoi/app/costmanagementserviceconfig.yaml
```

Required image fields (empty `repository`/`tag` → `InvalidImageName` / image `:`):

| Spec path | Purpose |
|-----------|---------|
| `costManagement.api/masu.image` | Koku |
| `rbac.image` | Insights RBAC |
| `auth.envoy.image` | Gateway |
| `ingress.image` | Upload handler |
| `ui.app.image` | UI |
| `ui.oauthProxy.image` | oauth2-proxy sidecar |

The sample CR sets `ros.enabled: false`. That skips ROS schema migrate, Kruize,
and ROS workers so a UI smoke does not need ROS/Kruize images or ClusterRole
escalation rights beyond what `manager-cluster-role` already grants for cleanup.
Set `ros.enabled: true` (and fill ROS/Kruize images) only when you need ROS.

### B4. Wait for reconcile

```bash
kubectl -n "$NAMESPACE" get cmsc "$CR_NAME" -w
kubectl -n "$NAMESPACE" describe cmsc "$CR_NAME"
```

Useful conditions: `DatabaseReady`, `CacheReady`, `KafkaReady`,
`SchemaUpToDate`, `AuthenticationReady` (gateway), `UIReady`, `Available`.

Phase is human-readable only — prefer conditions.

---

## Part C — Open the UI

```bash
oc -n "$NAMESPACE" get route "${CR_NAME}-ui" \
  -o jsonpath='https://{.spec.host}{"\n"}'
```

Open that URL. oauth2-proxy redirects to Keycloak; sign in with the realm user
(**`admin` / `admin`** from `deploy-rhbk.sh` defaults).

Sanity checks:

```bash
# Should 302 to Keycloak
curl -skI "https://$(oc -n "$NAMESPACE" get route "${CR_NAME}-ui" -o jsonpath='{.spec.host}')/"
```

---

## Common failures

| Symptom | Cause | Fix |
|---------|-------|-----|
| Probes fail / DB unreachable from laptop `make run` | No cluster DNS out-of-cluster | Use `./hack/deploy-incluster.sh` |
| `InvalidImageName` / image `:` | Missing `repository`/`tag` on UI, oauth-proxy, ingress, ROS, … | Set images in the CR (see sample) |
| Stuck creating Kruize `ClusterRole` (RBAC escalation) | ROS/Kruize applied with insufficient SA rights | Keep `ros.enabled: false` for UI smoke, or grant/hold the verbs Kruize’s role needs |
| `UIReady=False` | OAuth client Secret missing | Re-run `mirror-ui-oauth-secret.sh` |
| Login redirect_uri mismatch | Keycloak client built for wrong UI host | Re-run RHBK with `COST_MGMT_NAMESPACE` / `COST_MGMT_RELEASE_NAME` / `COST_MGMT_UI_BASE_URL` |
| ImagePullBackOff on amd64 node | arm64-only image | Rebuild with `--platform linux/amd64` |
| StorageClass list/watch forbidden | Stale cluster role | Re-apply `config/rbac/cluster_access_role.yaml` (`get;list;watch`) |

## Related docs

- [ownnamespace.md](ownnamespace.md) — install/watch model and RBAC shape
- [crc-testing.md](crc-testing.md) — local CRC / out-of-cluster `make run`
- [config/samples/byoi/README.md](../../config/samples/byoi/README.md) — fixture details, monitoring, teardown
