#!/usr/bin/env bash
# Deploy the operator locally against CRC for development testing.
# Usage: ./hack/deploy-crc.sh [namespace]
set -euo pipefail

NS="${1:-cost-onprem}"

echo "=== CRC dev deploy ==="
echo "Namespace: $NS"
echo "Cluster:   $(oc whoami --show-server)"
echo ""

# Create namespace
oc get namespace "$NS" &>/dev/null || oc create namespace "$NS"

# Install CRDs via kustomize (only the resources listed in config/crd/kustomization.yaml).
# Do not `oc apply -f config/crd/bases/` — that directory is a generator output and
# may contain leftover files from prior API-group renames.
echo "[1/3] Installing CRDs..."
make install

# Apply RBAC needed by the operator
echo "[2/3] Applying RBAC..."
# ClusterRole from generated role.yaml
oc apply -f config/rbac/role.yaml 2>/dev/null || true
# Simple binding for developer account (CRC default)
oc create clusterrolebinding koku-operator-dev \
  --clusterrole=manager-role \
  --serviceaccount="$NS:default" \
  2>/dev/null || echo "  (binding already exists)"

# Grant anyuid SCC so bundled PostgreSQL/Valkey pods can run with their
# required UIDs (postgres=999, valkey=999). Skip if not on OpenShift.
echo "[3/4] Granting anyuid SCC (bundled DB/Cache mode)..."
oc adm policy add-scc-to-user anyuid -z default -n "$NS" 2>/dev/null || true

echo "[4/4] Ready. Run the operator with:"
echo ""
echo "  NAMESPACE=$NS go run ./cmd/main.go --dev"
echo ""
echo "Then in another terminal, apply the sample CR:"
echo ""
echo "  oc apply -n $NS -f config/samples/service.costmanagement_v1alpha1_costmanagementserviceconfig.yaml"
echo ""
echo "Watch status:"
echo "  oc get costmanagementserviceconfig -n $NS -w"
echo "  oc describe costmanagementserviceconfig cost-management -n $NS"
