# Testing on CRC (CodeReady Containers / OpenShift Local)

CRC provides a single-node OpenShift cluster for local development. The
operator runs locally (out-of-cluster) and talks to CRC via kubeconfig.

## Prerequisites

- CRC installed (`brew install --cask crc` or from [developers.redhat.com](https://developers.redhat.com/products/openshift-local))
- Pull secret at `~/.crc-secret.json` (download from Red Hat console)
- `oc` CLI available (CRC ships one at `~/.crc/bin/oc/oc`)

## Start CRC

```bash
crc setup                          # first-time setup (downloads ~4 GB bundle)
crc start -p ~/.crc-secret.json    # start the cluster (~5–10 min)
```

Check status:

```bash
crc status
```

If the cluster becomes unreachable after being left running for a long time,
restart it:

```bash
pkill -f vfkit; pkill -f "crc daemon"
crc delete --force && crc setup && crc start -p ~/.crc-secret.json
```

## Log in

```bash
eval "$(crc oc-env)"
crc console --credentials          # prints kubeadmin password

oc login -u kubeadmin -p <password> https://api.crc.testing:6443 \
  --insecure-skip-tls-verify
```

## Install CRDs and RBAC

```bash
./hack/deploy-crc.sh cost-onprem
```

This script installs the CRD and grants the `default` ServiceAccount
`cluster-admin` in the target namespace. Run it once per CRC restart.

Alternatively, do it manually:

```bash
oc new-project cost-onprem
make manifests
oc apply -f config/crd/bases/
oc adm policy add-cluster-role-to-user cluster-admin \
  system:serviceaccount:cost-onprem:default
oc adm policy add-scc-to-user anyuid -z default -n cost-onprem
```

## Run the operator

```bash
NAMESPACE=cost-onprem go run ./cmd/main.go --dev \
  --health-probe-bind-address=:8082 \
  --metrics-bind-address=:8083
```

The operator reads `~/.kube/config` (set by `eval "$(crc oc-env)"`) and
watches the `cost-onprem` namespace.

## Apply a sample CR

In a second terminal:

```bash
eval "$(crc oc-env)"

# Bundled mode (DB + Cache provisioned by operator — dev only)
oc apply -n cost-onprem \
  -f config/samples/service.costmanagement_v1alpha1_costmanagementserviceconfig.yaml

# Watch reconciliation
oc get cmsc -n cost-onprem -w
oc describe cmsc cost-management -n cost-onprem
```

## Image note: arm64 vs amd64

CRC on Apple Silicon runs an **arm64** node. The production koku image
(`quay.io/redhat-services-prod/cost-mgmt-dev-tenant/koku:d8055ac`) is
**amd64-only** and segfaults under QEMU emulation.

Use the local arm64 build for testing:

```yaml
costManagement:
  api:
    image:
      repository: quay.io/martin_povolny/koku
      tag: "latest"
```

The bundled sample CR (`config/samples/..._costmanagementserviceconfig.yaml`)
already uses this image.

## Storage class

CRC's default storage class is `crc-csi-hostpath-provisioner`. The production
sample CR defaults to `ocs-storagecluster-ceph-rbd` (ODF). For CRC, leave
`global.storageClass` empty or set it explicitly:

```yaml
global:
  storageClass: ""   # uses cluster default (crc-csi-hostpath-provisioner)
```

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `TLS handshake timeout` | CRC cluster hung under load | Restart CRC |
| `Permission denied` on PVC mount | fsGroup not set | `anyuid` SCC already granted by deploy-crc.sh; check `fsGroup` in pod SC |
| `No module named listener` | Wrong container command | Fixed — uses `python manage.py listener` |
| `Unable to configure handler 'file'` | Django file log handler on read-only FS | Fixed — `kokuAppContainerSC()` does not set `readOnlyRootFilesystem` |
| Migration segfault | amd64 image on arm64 node | Use arm64 image (see above) |
