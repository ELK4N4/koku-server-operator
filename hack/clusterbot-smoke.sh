#!/usr/bin/env bash
# Cluster Bot / lab day-one smoke: BYOI infra + Redpanda + app Secrets + smoke CR.
# Does NOT build or deploy the operator — prints the next in-cluster step.
#
# Usage (from repo root):
#   ./hack/clusterbot-smoke.sh
#   NAMESPACE=cost-byoi INFRA_NAMESPACE=cost-byoi-infra ./hack/clusterbot-smoke.sh
#
# Next:
#   IMG=quay.io/<you>/koku-service-operator:<tag> ./hack/deploy-incluster.sh "$NAMESPACE"
#
# See docs/development/clusterbot.md
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

NAMESPACE="${NAMESPACE:-cost-byoi}"
CR_NAME="${CR_NAME:-cost-management}"
INFRA_NAMESPACE="${INFRA_NAMESPACE:-cost-byoi-infra}"
SMOKE_CR="${SMOKE_CR:-config/samples/byoi/app/costmanagementserviceconfig-smoke.yaml}"
SECRETS_YAML="${SECRETS_YAML:-config/samples/byoi/app/secrets.yaml}"

KUBECTL="${KUBECTL:-kubectl}"
if ! command -v "$KUBECTL" >/dev/null 2>&1; then
  if command -v oc >/dev/null 2>&1; then
    KUBECTL=oc
  else
    echo "error: kubectl or oc is required" >&2
    exit 1
  fi
fi

need_ns() {
  local ns="$1"
  if ! "$KUBECTL" get ns "$ns" >/dev/null 2>&1; then
    "$KUBECTL" create ns "$ns"
  fi
}

echo "=== Cluster Bot smoke (Redpanda BYOI) ==="
echo "App NS:    $NAMESPACE"
echo "Infra NS:  $INFRA_NAMESPACE"
echo "CR:        $CR_NAME"
echo "Cluster:   $($KUBECTL config current-context 2>/dev/null || echo unknown)"
echo ""
echo "NOTE: Do not use laptop 'make run' against this BYOI — *.svc is not"
echo "resolvable from your machine. Use ./hack/deploy-incluster.sh after this."
echo ""

# ---------------------------------------------------------------------------
# CRDs + RBAC (so the CR can be applied even before the manager is up)
# ---------------------------------------------------------------------------
echo "[1/4] CRDs + OwnNamespace RBAC (deploy-dev.sh)..."
./hack/deploy-dev.sh "$NAMESPACE"

# ---------------------------------------------------------------------------
# Infra + Redpanda
# ---------------------------------------------------------------------------
echo "[2/4] Postgres / Valkey / MinIO + Redpanda in ${INFRA_NAMESPACE}..."
need_ns "$INFRA_NAMESPACE"
if command -v oc >/dev/null 2>&1; then
  oc adm policy add-scc-to-user anyuid -z byoi-infra -n "$INFRA_NAMESPACE" 2>/dev/null || true
fi

TMP_INFRA="$(mktemp -d)"
cleanup() { rm -rf "$TMP_INFRA"; }
trap cleanup EXIT
cp -a "$ROOT/config/samples/byoi/infra/." "$TMP_INFRA/"
find "$TMP_INFRA" -type f \( -name '*.yaml' -o -name '*.yml' \) \
  -exec sed -i.bak "s/cost-byoi-infra/${INFRA_NAMESPACE}/g" {} +
find "$TMP_INFRA" -name '*.bak' -delete
"$KUBECTL" apply -k "$TMP_INFRA"
# Redpanda is optional in kustomize — apply explicitly (also retarget namespace).
sed "s/cost-byoi-infra/${INFRA_NAMESPACE}/g" "$ROOT/config/samples/byoi/infra/kafka.yaml" \
  | "$KUBECTL" apply -f -

"$KUBECTL" -n "$INFRA_NAMESPACE" rollout status deploy/postgresql --timeout=300s
"$KUBECTL" -n "$INFRA_NAMESPACE" rollout status deploy/valkey --timeout=180s
"$KUBECTL" -n "$INFRA_NAMESPACE" rollout status deploy/minio --timeout=180s
"$KUBECTL" -n "$INFRA_NAMESPACE" rollout status deploy/kafka --timeout=180s
"$KUBECTL" -n "$INFRA_NAMESPACE" wait --for=condition=complete job/minio-init --timeout=180s

echo "  Infra Ready: postgresql, valkey, minio, kafka (Redpanda)"

# ---------------------------------------------------------------------------
# Secrets + smoke CR
# ---------------------------------------------------------------------------
echo "[3/4] App Secrets + smoke CR..."
need_ns "$NAMESPACE"
if [[ -f "$SECRETS_YAML" ]]; then
  # Retarget namespace if the fixture hard-codes cost-byoi.
  sed "s/namespace: cost-byoi/namespace: ${NAMESPACE}/g" "$SECRETS_YAML" | "$KUBECTL" apply -f -
else
  echo "error: secrets file not found: $SECRETS_YAML" >&2
  exit 1
fi

DOMAIN="$("$KUBECTL" get ingress.config.openshift.io cluster -o jsonpath='{.spec.domain}' 2>/dev/null || true)"
TMP_CR="$(mktemp)"
cp "$SMOKE_CR" "$TMP_CR"
sed -i.bak "s/namespace: cost-byoi/namespace: ${NAMESPACE}/g" "$TMP_CR"
sed -i.bak "s/name: cost-management/name: ${CR_NAME}/g" "$TMP_CR"
sed -i.bak "s/cost-byoi-infra/${INFRA_NAMESPACE}/g" "$TMP_CR"
if [[ -n "$DOMAIN" ]]; then
  sed -i.bak "s/apps.cluster.example.com/${DOMAIN}/g" "$TMP_CR"
fi
rm -f "${TMP_CR}.bak"
"$KUBECTL" apply -f "$TMP_CR"
rm -f "$TMP_CR"

echo "  CR applied: ${NAMESPACE}/${CR_NAME}"
echo "  Kafka bootstrap: kafka.${INFRA_NAMESPACE}.svc.cluster.local:9092 (Redpanda)"
if [[ -n "$DOMAIN" ]]; then
  echo "  clusterDomain: ${DOMAIN}"
fi

# ---------------------------------------------------------------------------
# Next step
# ---------------------------------------------------------------------------
echo ""
echo "[4/4] Next — run the operator IN-CLUSTER (not laptop go run):"
echo ""
echo "  export IMG=quay.io/<you>/koku-service-operator:clusterbot"
echo "  docker buildx build --platform linux/amd64 -t \"\$IMG\" --push ."
echo "  IMG=\"\$IMG\" ./hack/deploy-incluster.sh ${NAMESPACE}"
echo ""
echo "Then:"
echo "  oc -n ${NAMESPACE} get cmsc ${CR_NAME} -w"
echo "  oc -n ${NAMESPACE} get cmsc ${CR_NAME} -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){\"\\n\"}{end}'"
echo ""
echo "Without Keycloak, AuthenticationReady / UIReady may stay False — that is OK"
echo "for day-one core smoke. Full UI path: docs/development/pre-prod-install.md"
echo "Checklist: docs/development/clusterbot.md"
