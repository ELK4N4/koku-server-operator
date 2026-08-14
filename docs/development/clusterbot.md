# Cluster Bot day-one

Short path to get a `CostManagementServiceConfig` reconciling on a **Cluster Bot**
(or any remote OpenShift) lab cluster. OwnNamespace: operator install namespace
**equals** CR namespace.

For a fuller BYOI → UI walkthrough (AMQ Streams + Keycloak), see
[pre-prod-install.md](pre-prod-install.md). For laptop **CRC** with bundled DB,
see [crc-testing.md](crc-testing.md).

## Do not use laptop `make run` / `go run` against Cluster Bot BYOI

BYOI hosts are `*.svc.cluster.local`. A controller running on your laptop
**cannot** resolve or TCP-probe them — DatabaseReady / CacheReady / KafkaReady
stay False forever.

Use **`./hack/deploy-incluster.sh`** so the manager runs inside the cluster.

`make run` is for CRC / local kubeconfig when dependencies are reachable from
the host (or you use bundled `database.deploy: true`).

## Happy path (Redpanda — no AMQ Streams)

One-shot infra + secrets + smoke CR (does **not** install the operator image):

```bash
export NAMESPACE=cost-byoi
./hack/clusterbot-smoke.sh
```

Then build/push an **linux/amd64** operator image and run it in-cluster:

```bash
export IMG=quay.io/<you>/koku-service-operator:clusterbot
docker buildx build --platform linux/amd64 -t "$IMG" --push .
IMG="$IMG" ./hack/deploy-incluster.sh "$NAMESPACE"
```

`deploy-incluster.sh` calls `deploy-dev.sh` (CRDs + RBAC), creates a lab webhook
TLS Secret, and rolls out the manager. Watch:

```bash
oc -n "$NAMESPACE" get cmsc -w
oc -n "$NAMESPACE" describe cmsc cost-management
oc -n "$NAMESPACE" logs -f deploy/koku-service-operator
```

### Manual equivalent

```bash
export NAMESPACE=cost-byoi INFRA_NAMESPACE=cost-byoi-infra

# 1) CRDs + OwnNamespace RBAC
./hack/deploy-dev.sh "$NAMESPACE"   # alias: ./hack/deploy-crc.sh

# 2) BYOI infra (Postgres, Valkey, MinIO) + lightweight Redpanda
oc get ns "$INFRA_NAMESPACE" >/dev/null 2>&1 || oc create ns "$INFRA_NAMESPACE"
oc adm policy add-scc-to-user anyuid -z byoi-infra -n "$INFRA_NAMESPACE" 2>/dev/null || true
kubectl apply -k config/samples/byoi/infra
kubectl apply -f config/samples/byoi/infra/kafka.yaml
kubectl -n "$INFRA_NAMESPACE" rollout status deploy/postgresql --timeout=300s
kubectl -n "$INFRA_NAMESPACE" rollout status deploy/valkey --timeout=180s
kubectl -n "$INFRA_NAMESPACE" rollout status deploy/minio --timeout=180s
kubectl -n "$INFRA_NAMESPACE" rollout status deploy/kafka --timeout=180s
kubectl -n "$INFRA_NAMESPACE" wait --for=condition=complete job/minio-init --timeout=180s

# 3) App Secrets + smoke CR (Redpanda bootstrapServers already set)
kubectl apply -f config/samples/byoi/app/secrets.yaml
# Patch clusterDomain to your apps.* domain:
DOMAIN=$(oc get ingress.config.openshift.io cluster -o jsonpath='{.spec.domain}')
sed "s/apps.cluster.example.com/${DOMAIN}/" \
  config/samples/byoi/app/costmanagementserviceconfig-smoke.yaml | oc apply -f -

# 4) In-cluster operator (not laptop go run)
IMG="$IMG" ./hack/deploy-incluster.sh "$NAMESPACE"
```

If you already have AMQ Streams Kafka, point
`spec.kafka.bootstrapServers` at that bootstrap (see the default
`costmanagementserviceconfig.yaml`) instead of Redpanda.

## Stage checklist

| Stage | How to verify | OK if still False? |
|-------|---------------|--------------------|
| **Infra Ready** | `oc -n cost-byoi-infra get deploy` — postgresql, valkey, minio, kafka (Redpanda) Ready; minio-init Job Complete | — |
| **CR applied** | `oc -n cost-byoi get cmsc` shows the CR | — |
| **Operator reconciling** | `deploy/koku-service-operator` 1/1; logs show reconcile; `status.phase` Progressing or Ready | — |
| **Discovery / storage** | `DiscoveryComplete`, `StorageReady` True | Rarely OK False — fix S3 secret / endpoint |
| **DB / cache / kafka** | `DatabaseReady`, `CacheReady`, `KafkaReady` True | Must be True for migrations |
| **Schema** | `SchemaUpToDate` True | Must become True for app Deployments |
| **Auth / UI without Keycloak** | `AuthenticationReady`, `UIReady` may stay False | **Yes** for day-one core smoke — deploy Keycloak + mirror OAuth secret for UI login ([pre-prod-install.md](pre-prod-install.md)) |
| **ROS** | `ROSEnabled=False` when `ros.enabled: false` | Expected — no ROS/Kruize objects |
| **Available** | `Available=True` when core stack is up | Goal for day-one once schema + core Deployments are healthy |

Reading conditions:

```bash
oc -n cost-byoi get cmsc cost-management -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

Phase is human-readable only — prefer conditions.

## Local-run DX (CRC / laptop)

```bash
./hack/deploy-dev.sh cost-onprem
NAMESPACE=cost-onprem IMG=quay.io/project-koku/koku-service-operator:v0.0.1 make run
```

- **`--operator-image` is required** (wait-for init containers). `make run` passes
  `--operator-image=$(IMG)` and fails the recipe if `NAMESPACE` is unset.
- **`--dev` skips admission webhook registration** so you do not need TLS certs
  under `/tmp/k8s-webhook-server/serving-certs`. In-cluster lab deploys still
  mount a self-signed Secret via `deploy-incluster.sh` (production/OLM uses
  cert-manager).
- Missing `--operator-image` exits immediately with examples on stderr.

## Scripts

| Script | Role |
|--------|------|
| `hack/deploy-dev.sh` | CRDs + OwnNamespace RBAC (+ `deploy-crc.sh` alias) |
| `hack/clusterbot-smoke.sh` | Redpanda BYOI infra + secrets + smoke CR |
| `hack/deploy-incluster.sh` | In-cluster manager + webhook TLS mount |
| `hack/deploy-byoi.sh` | Full lab deps (AMQ Streams + Keycloak) |

## Tear down

```bash
oc delete cmsc -n cost-byoi --all --ignore-not-found
oc delete ns cost-byoi cost-byoi-infra --ignore-not-found
# If you used AMQ Streams / Keycloak instead of the smoke path:
#   ./config/samples/byoi/deploy-kafka.sh cleanup
#   ./scripts/deploy-rhbk.sh cleanup   # from cost-onprem-chart or repo scripts/
```
