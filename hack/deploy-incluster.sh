#!/usr/bin/env bash
# OwnNamespace in-cluster deploy: CRDs + RBAC + manager Deployment in the
# CostManagementServiceConfig namespace (install NS == CR NS).
#
# Prefer this over `make run` when the CR points at in-cluster BYOI hosts
# (*.svc.cluster.local) — those names are not resolvable from a laptop.
#
# Usage (from repo root):
#   IMG=quay.io/example/koku-service-operator:tag ./hack/deploy-incluster.sh cost-byoi
#   IMG=... ./hack/deploy-incluster.sh cost-tests
#
# Build an amd64 image for typical OpenShift nodes:
#   docker buildx build --platform linux/amd64 -t "$IMG" --push .
#
set -euo pipefail

NS="${1:-cost-byoi}"
IMG="${IMG:?IMG is required (e.g. quay.io/<org>/koku-service-operator:<tag>)}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if ! command -v oc >/dev/null 2>&1; then
  echo "error: oc is required" >&2
  exit 1
fi

echo "=== In-cluster OwnNamespace deploy ==="
echo "Namespace: $NS"
echo "Image:     $IMG"
echo "Cluster:   $(oc whoami --show-server)"
echo ""

# CRDs + RoleBinding (default SA) + ClusterRoleBinding + anyuid SCC.
./hack/deploy-crc.sh "$NS"

echo "[in-cluster] Applying manager Deployment (SA=default)..."
oc apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: koku-service-operator
  namespace: ${NS}
  labels:
    app.kubernetes.io/name: koku-service-operator
    control-plane: controller-manager
spec:
  replicas: 1
  selector:
    matchLabels:
      control-plane: controller-manager
      app.kubernetes.io/name: koku-service-operator
  template:
    metadata:
      labels:
        control-plane: controller-manager
        app.kubernetes.io/name: koku-service-operator
    spec:
      serviceAccountName: default
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
      - name: manager
        image: ${IMG}
        imagePullPolicy: Always
        command: ["/manager"]
        args:
        - --leader-elect
        - --health-probe-bind-address=:8081
        - --operator-image=${IMG}
        env:
        - name: NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop: ["ALL"]
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8081
          initialDelaySeconds: 15
          periodSeconds: 20
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8081
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          requests:
            cpu: 10m
            memory: 64Mi
EOF

oc -n "$NS" rollout status deploy/koku-service-operator --timeout=180s

echo ""
echo "Operator is running in ${NS}."
echo "Next: apply Secrets + CostManagementServiceConfig in the same namespace,"
echo "mirror the UI OAuth client Secret, then open the UI Route."
echo "See docs/development/pre-prod-install.md"
