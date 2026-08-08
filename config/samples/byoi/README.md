# BYOI test fixture

Dev-only manifests that stand up external infrastructure in one namespace and a
separate `CostManagementServiceConfig` (BYOI mode) in another. The operator
does **not** provision DB/cache; it only connects.

| Namespace | Contents |
|-----------|----------|
| `cost-byoi-infra` | PostgreSQL, Valkey, Redpanda (Kafka API), MinIO |
| `cost-byoi` | App Secrets + `CostManagementServiceConfig` |

Keycloak/OIDC is intentionally omitted (placeholder URL in the CR).

**Credentials in these YAMLs are fixed test values — not for production.**

## Prerequisites

- Operator installed and running on the cluster
- `kubectl` (or `oc`) with cluster-admin (or enough rights for Deployments/PVCs/SCC)
- Default StorageClass available

On OpenShift, grant `anyuid` to the infra ServiceAccount (official postgres /
Kafka / MinIO images need it):

```bash
oc adm policy add-scc-to-user anyuid -z byoi-infra -n cost-byoi-infra
```

## Apply

```bash
# 1. Infrastructure
kubectl apply -k config/samples/byoi/infra

# Wait until pods are Ready (adjust timeout as needed)
kubectl -n cost-byoi-infra rollout status deploy/postgresql --timeout=180s
kubectl -n cost-byoi-infra rollout status deploy/valkey --timeout=120s
kubectl -n cost-byoi-infra rollout status deploy/kafka --timeout=180s
kubectl -n cost-byoi-infra rollout status deploy/minio --timeout=120s
kubectl -n cost-byoi-infra wait --for=condition=complete job/minio-init --timeout=120s

# 2. App secrets FIRST (must exist before the operator reconciles the CR),
#    then the BYOI CostManagementServiceConfig
kubectl apply -f config/samples/byoi/app/secrets.yaml
kubectl apply -f config/samples/byoi/app/costmanagementserviceconfig.yaml
# Or together after secrets are present:
# kubectl apply -k config/samples/byoi/app
```

## Watch

```bash
kubectl -n cost-byoi get cmsc cost-management -w
kubectl -n cost-byoi describe cmsc cost-management
kubectl -n op-sdk-scaffold-system logs deploy/op-sdk-scaffold-controller-manager -f
```

## Monitoring (optional)

Standalone Prometheus + Grafana in `cost-byoi-infra`. No Prometheus Operator required — uses static scrape configs targeting port 9000 on each service.

```bash
kubectl apply -k config/samples/byoi/monitoring
```

Access Grafana (anonymous admin, no login needed):
```bash
kubectl -n cost-byoi-infra port-forward svc/grafana 3000:3000
# open http://localhost:3000
```

Access Prometheus:
```bash
kubectl -n cost-byoi-infra port-forward svc/prometheus 9090:9090
# open http://localhost:9090
```

> **Note:** Scrape targets reference CR name `cost-management` in namespace `cost-byoi`.
> If your CR name or namespace differs, edit `monitoring/prometheus.yaml` before applying.

When COST-7692 ServiceMonitors are implemented, this fixture can be replaced by enabling
OpenShift user workload monitoring (`enableUserWorkload: true` in `cluster-monitoring-config`).

## Tear down

```bash
kubectl delete -k config/samples/byoi/app --ignore-not-found
kubectl delete -k config/samples/byoi/infra --ignore-not-found
kubectl delete -k config/samples/byoi/monitoring --ignore-not-found
```

## Endpoints (from `cost-byoi`)

| Service | Address |
|---------|---------|
| PostgreSQL | `postgresql.cost-byoi-infra.svc.cluster.local:5432` |
| Valkey | `valkey.cost-byoi-infra.svc.cluster.local:6379` |
| Kafka (Redpanda, emptyDir) | `kafka.cost-byoi-infra.svc.cluster.local:9092` |
| MinIO (S3) | `minio.cost-byoi-infra.svc.cluster.local:9000` (HTTP) |
| Prometheus | `prometheus.cost-byoi-infra.svc.cluster.local:9090` (when monitoring applied) |
| Grafana | `grafana.cost-byoi-infra.svc.cluster.local:3000` (when monitoring applied) |
