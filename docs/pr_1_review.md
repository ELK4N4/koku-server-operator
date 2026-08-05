# PR #1 Code Review — [COST-7677] Initialize operator scaffold with operator-sdk

**Author:** Elkana (ELK4N4) | **Size:** +3338 lines, 57 files | **Type:** Pure scaffold

---

## Overview

This PR bootstraps the `koku-server-operator` project using `operator-sdk v1.42.2`. It generates standard Go operator boilerplate: a `CostManagement` CRD, a stub reconciler, RBAC manifests, CI workflows, a Makefile, a Dockerfile, and a devcontainer. No application logic is present yet — this is entirely scaffold output.

---

## What's Good

- **Modern, up-to-date toolchain:** Go 1.24, controller-runtime v0.21.0, k8s v0.33.0 — all current.
- **Security-conscious defaults:** HTTP/2 disabled out of the box (protects against GHSA-qppj-fm5r-hxr3 / GHSA-4374-p667-p6c8), distroless base image, non-root UID 65532, `runAsNonRoot: true`, `seccompProfile: RuntimeDefault`, all capabilities dropped.
- **Proper RBAC tiering:** Separate `admin`, `editor`, and `viewer` ClusterRoles for `CostManagement`, plus an authenticated metrics endpoint.
- **Good linter config** (`.golangci.yml`): errcheck, staticcheck, gocyclo, revive, and more — a solid set.
- **DevContainer support** for onboarding contributors.
- **Test infrastructure scaffolded:** Ginkgo/Gomega unit test + suite, plus an e2e test stub with Kind.

---

## Issues

### Security / Reliability

- **`post-install.sh` — unpinned tool versions (`curl .../latest/`):**
  ```sh
  curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64
  curl -L -o kubebuilder https://go.kubebuilder.io/dl/latest/linux/amd64
  ```
  Both download `latest` with no checksum verification. This is a supply-chain risk: a compromised or changed upstream silently contaminates the dev environment. Pin to specific versions and verify checksums.

- **`config/prometheus/monitor.yaml` ships `insecureSkipVerify: true`:**
  The TODO comment acknowledges this, but it's a prod-unsafe default that should be removed or gate-kept more clearly. Someone will deploy this and never revisit it.

- **`post-install.sh` missing `set -e` / `pipefail`:**
  Only `set -x` is set. A failed `curl` or `kind` install will silently continue. Should be `set -eo pipefail`.

### CI / Workflow

- **`go mod tidy` in CI workflows (`test.yml`, `test-e2e.yml`):**
  Running `go mod tidy` in CI can silently mutate `go.sum`. If the module graph is already tidy, this is a no-op but it's a risky habit. Remove it; CI should verify, not modify.

- **Workflows trigger on all branches for `push:`:**
  No branch filter means every push to every branch (including WIP branches) triggers lint, tests, and e2e. Consider `branches: [main]` for `push:` events to cut redundant runs.

### Placeholder / TODO Debt

- **`CostManagementSpec` still contains the scaffold placeholder `Foo` field** (`api/v1alpha1/costmanagement_types.go:27`). This ships in the CRD YAML too. Should be removed before any API consumers form expectations.

- **`config/samples/cost_v1alpha1_costmanagement.yaml`** has only `# TODO(user): Add fields here`. The sample is useless until `Foo` (or real fields) is filled in.

- **Reconciler is a no-op** (`internal/controller/costmanagement_controller.go:55`). Expected for a scaffold PR, but worth confirming in the PR description that there is a follow-on ticket to implement the reconcile loop.

- **`cmd/main.go` hardcodes `Development: true`** for the logger (line 1015). This enables debug-level output in all environments. Should default to `false` and be controlled via a flag.

### Makefile / Config

- **`IMAGE_TAG_BASE ?= redhat.com/koku-server-operator`** in `Makefile:333`. `redhat.com` is not a real container registry. Should probably be `quay.io/project-koku/koku-server-operator`.

- **`LeaderElectionID: "1898b328.redhat.com"`** is a kubebuilder-generated random ID. It works, but consider changing it to something descriptive like `koku-server-operator.cost.redhat.com` for observability.

- **`post-install.sh` — `docker network create` may fail on re-runs:**
  ```sh
  docker network create -d=bridge --subnet=172.19.0.0/24 kind
  ```
  No `|| true` guard. This will error if the network already exists and halt the devcontainer setup.

---

## Summary

This is a clean scaffold with good security defaults and a reasonable CI skeleton. Before it's usable:

1. Remove the `Foo` placeholder from the API types and CRD.
2. Pin tool versions and add checksums in `post-install.sh`.
3. Fix `post-install.sh` to use `set -eo pipefail` and guard the `docker network create`.
4. Remove `go mod tidy` from CI workflows.
5. Fix `IMAGE_TAG_BASE` to a real registry.
6. Change `Development: true` to `false` or make it flag-controlled.
