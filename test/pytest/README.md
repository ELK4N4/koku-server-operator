# Operator pytest suite (COST-7697)

Pytest suite ported from the deprecated cost-onprem Helm chart. These tests
target an **operator-deployed** Cost Management stack (`CostManagementServiceConfig`).

## Prerequisites

1. OpenShift cluster access (`oc login`)
2. Ability to push to the OpenShift integrated registry (`oc registry login`)
3. Cluster can pull workload images used in `config/samples/pytest/`

## Full harness (recommended)

```bash
# From repo root — builds/pushes operator to the internal registry, make deploy,
# installs RHBK + Kafka, mirrors UI OAuth secret, applies CMSC, waits Ready,
# runs pytest (performance excluded by default), deletes the CR.
./scripts/deploy-test.sh --namespace cost-onprem

# Skip Playwright UI:
./scripts/deploy-test.sh --namespace cost-onprem --no-ui

# Operator already deployed:
./scripts/deploy-test.sh --skip-operator-deploy --namespace cost-onprem
```

## Tests only (stack already Ready)

```bash
NAMESPACE=cost-onprem CR_NAME=cost-management ./scripts/run-pytest.sh
NAMESPACE=cost-onprem CR_NAME=cost-management ./scripts/run-pytest.sh --no-ui
NAMESPACE=cost-onprem CR_NAME=cost-management ./scripts/run-pytest.sh --smoke
NAMESPACE=cost-onprem CR_NAME=cost-management ./scripts/run-pytest.sh --auth --e2e
```

## Environment

| Variable | Default | Meaning |
|----------|---------|---------|
| `NAMESPACE` | `cost-onprem` | Workload namespace |
| `CR_NAME` | `cost-management` | CMSC metadata.name (resource name prefix) |
| `KEYCLOAK_NAMESPACE` | `keycloak` | RHBK namespace |
| `PYTHON` | auto (`python3.11` preferred) | Python 3.10–3.12 (CI uses 3.11) |
| `IMG` | OpenShift internal registry image | Operator controller image for `make deploy` |

## Markers

- Suites: `auth`, `infrastructure`, `cost_management`, `sources`, `ros`, `e2e`, `ui`, `api`, `interpod`
- Types: `component`, `integration`, `smoke`, `slow`
- Performance files exist under `suites/performance/` but are **excluded by default** (`not performance`)

There is **no** Helm suite and **no** `HELM_RELEASE_NAME` support.

## Naming notes

Operator resources are `{CR_NAME}-*`. Celery workers use
`app.kubernetes.io/component=cost-worker-<queue>` (not a shared `cost-worker` label).
Cache pods use `component=cache`.
