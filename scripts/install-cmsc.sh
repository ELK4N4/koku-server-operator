#!/usr/bin/env bash
# Install Cost Management via CostManagementServiceConfig (operator analogue of install-helm-chart.sh).
#
# Mirrors UI OAuth secret, applies the CMSC sample (with live Kafka/Keycloak patches),
# and waits for status.phase=Ready.
#
# Usage:
#   ./scripts/install-cmsc.sh [OPTIONS]
#
# Options:
#   --skip-deploy              Skip apply + wait (mirror OAuth only)
#   --skip-wait                Apply CMSC but do not wait for Ready
#   --verbose                  Verbose wait logging
#   --help                     Show help
#
# Environment:
#   NAMESPACE                  Workload namespace (default: cost-onprem)
#   CR_NAME                    CMSC name (default: cost-management)
#   KEYCLOAK_NAMESPACE         (default: keycloak)
#   KAFKA_NAMESPACE            (default: kafka)
#   KAFKA_BOOTSTRAP            Override Kafka bootstrap (auto-detected if unset)
#   KEYCLOAK_URL               Override Keycloak URL (auto-detected if unset)
#   KEYCLOAK_INSECURE_SKIP_VERIFY  (default: true)
#   READY_TIMEOUT              Wait for Ready (default: 1800s)
#   OPERATOR_NS                Controller namespace for diagnostics (default: koku-service-operator-system)
#   CMSC_SAMPLE                Override sample YAML path

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

NAMESPACE="${NAMESPACE:-cost-onprem}"
CR_NAME="${CR_NAME:-cost-management}"
KEYCLOAK_NAMESPACE="${KEYCLOAK_NAMESPACE:-keycloak}"
KAFKA_NAMESPACE="${KAFKA_NAMESPACE:-kafka}"
OPERATOR_NS="${OPERATOR_NS:-koku-service-operator-system}"
READY_TIMEOUT="${READY_TIMEOUT:-1800}"
KEYCLOAK_INSECURE_SKIP_VERIFY="${KEYCLOAK_INSECURE_SKIP_VERIFY:-true}"
CMSC_SAMPLE="${CMSC_SAMPLE:-${PROJECT_ROOT}/config/samples/pytest/costmanagementserviceconfig.yaml}"

SKIP_DEPLOY=false
SKIP_WAIT=false
VERBOSE=false

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $*"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
log_step() { echo -e "\n${BLUE}==>${NC} $*"; }

usage() {
    sed -n '/^# Install Cost Management/,/^set -euo pipefail$/p' "$0" | grep '^#' | sed 's/^# \?//'
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip-deploy) SKIP_DEPLOY=true; shift ;;
        --skip-wait) SKIP_WAIT=true; shift ;;
        --verbose) VERBOSE=true; shift ;;
        --help|-h) usage ;;
        *)
            log_error "Unknown option: $1"
            usage ;;
    esac
done

require_oc() {
    if ! command -v oc >/dev/null 2>&1; then
        log_error "oc CLI required"
        exit 1
    fi
    if ! oc whoami >/dev/null 2>&1; then
        log_error "Not logged into OpenShift"
        exit 1
    fi
}

# Resolve in-cluster Keycloak backchannel URL from live RHBK service/CR state.
# deploy-rhbk.sh sets httpEnabled=true and exposes keycloak-service on :8080 only;
# use HTTPS :8443 only when the Service actually publishes that port.
detect_keycloak_url() {
    local svc_name=""
    if oc get svc -n "${KEYCLOAK_NAMESPACE}" keycloak-service >/dev/null 2>&1; then
        svc_name="keycloak-service"
    elif oc get svc -n "${KEYCLOAK_NAMESPACE}" keycloak >/dev/null 2>&1; then
        svc_name="keycloak"
    else
        log_warning "Keycloak service not found in ${KEYCLOAK_NAMESPACE}; defaulting to HTTPS :8443"
        echo "https://keycloak.${KEYCLOAK_NAMESPACE}.svc.cluster.local:8443"
        return 0
    fi

    local host="${svc_name}.${KEYCLOAK_NAMESPACE}.svc.cluster.local"
    local has_8443 has_8080 http_enabled
    has_8443="$(oc get svc "${svc_name}" -n "${KEYCLOAK_NAMESPACE}" \
        -o jsonpath='{range .spec.ports[?(@.port==8443)]}{@.port}{end}' 2>/dev/null || true)"
    has_8080="$(oc get svc "${svc_name}" -n "${KEYCLOAK_NAMESPACE}" \
        -o jsonpath='{range .spec.ports[?(@.port==8080)]}{@.port}{end}' 2>/dev/null || true)"
    http_enabled="$(oc get keycloak keycloak -n "${KEYCLOAK_NAMESPACE}" \
        -o jsonpath='{.spec.http.httpEnabled}' 2>/dev/null || true)"

    if [[ -n "${has_8443}" ]]; then
        echo "https://${host}:8443"
    elif [[ "${http_enabled}" == "true" && -n "${has_8080}" ]]; then
        echo "http://${host}:8080"
    elif [[ -n "${has_8080}" ]]; then
        echo "http://${host}:8080"
    else
        log_warning "Could not detect Keycloak ports on ${svc_name}; defaulting to HTTPS :8443"
        echo "https://${host}:8443"
    fi
}

mirror_ui_oauth() {
    log_step "Mirroring UI OAuth client secret"
    local mirror="${PROJECT_ROOT}/config/samples/byoi/mirror-ui-oauth-secret.sh"
    if [[ ! -x "${mirror}" ]]; then
        chmod +x "${mirror}" 2>/dev/null || true
    fi
    NAMESPACE="${NAMESPACE}" CR_NAME="${CR_NAME}" KEYCLOAK_NAMESPACE="${KEYCLOAK_NAMESPACE}" \
        bash "${mirror}" || {
            log_error "UI OAuth secret mirror failed (required for UIReady / phase Ready)"
            exit 1
        }
}

apply_cmsc() {
    log_step "Applying CostManagementServiceConfig ${CR_NAME}"
    if [[ ! -f "${CMSC_SAMPLE}" ]]; then
        log_error "Missing ${CMSC_SAMPLE}"
        exit 1
    fi

    local tmp
    tmp="$(mktemp)"
    local bootstrap="${KAFKA_BOOTSTRAP:-}"
    if [[ -z "${bootstrap}" ]]; then
        bootstrap="$(oc get kafka -n "${KAFKA_NAMESPACE}" -o jsonpath='{.items[0].status.listeners[0].bootstrapServers}' 2>/dev/null || true)"
    fi
    if [[ -z "${bootstrap}" ]]; then
        bootstrap="cost-onprem-kafka-kafka-bootstrap.${KAFKA_NAMESPACE}.svc.cluster.local:9092"
        log_warning "Using default Kafka bootstrap: ${bootstrap}"
    else
        log_info "Kafka bootstrap: ${bootstrap}"
    fi

    local keycloak_url="${KEYCLOAK_URL:-}"
    local keycloak_insecure="${KEYCLOAK_INSECURE_SKIP_VERIFY}"
    if [[ -z "${keycloak_url}" ]]; then
        keycloak_url="$(detect_keycloak_url)"
    fi
    log_info "Keycloak URL: ${keycloak_url} (insecureSkipVerify=${keycloak_insecure})"

    # shellcheck disable=SC2016
    sed \
        -e "s/namespace: cost-onprem/namespace: ${NAMESPACE}/" \
        -e "s/name: cost-management/name: ${CR_NAME}/" \
        -e "s|bootstrapServers: \".*\"|bootstrapServers: \"${bootstrap}\"|" \
        -e "s|url: \".*keycloak.*\"|url: \"${keycloak_url}\"|" \
        "${CMSC_SAMPLE}" > "${tmp}"

    if [[ "${keycloak_url}" == https://* ]]; then
        if grep -q 'insecureSkipVerify:' "${tmp}"; then
            sed -i "s|insecureSkipVerify:.*|insecureSkipVerify: ${keycloak_insecure}|" "${tmp}"
        else
            awk -v flag="${keycloak_insecure}" '
              /^[[:space:]]*keycloak:/ { in_kc=1; print; next }
              in_kc && /^[[:space:]]*realm:/ {
                print
                print "      tls:"
                print "        insecureSkipVerify: " flag
                in_kc=0
                next
              }
              { print }
            ' "${tmp}" > "${tmp}.tls" && mv "${tmp}.tls" "${tmp}"
        fi
    elif command -v yq >/dev/null 2>&1; then
        yq -i 'del(.spec.auth.keycloak.tls)' "${tmp}"
    else
        sed -i '/^[[:space:]]\{6\}tls:/,/^[[:space:]]\{8\}insecureSkipVerify:/d' "${tmp}"
    fi

    oc apply -f "${tmp}"
    rm -f "${tmp}"
    log_success "CMSC applied"
}

wait_cmsc_ready() {
    log_step "Waiting for CMSC ${CR_NAME} status.phase=Ready (timeout ${READY_TIMEOUT}s)"
    local end=$((SECONDS + READY_TIMEOUT))
    local empty_ticks=0
    local last_phase=""
    while (( SECONDS < end )); do
        local phase msg
        phase="$(oc get cmsc "${CR_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
        msg="$(oc get cmsc "${CR_NAME}" -n "${NAMESPACE}" -o jsonpath='{range .status.conditions[?(@.status=="False")]}{.type}={.reason}: {.message}; {end}' 2>/dev/null || true)"
        if [[ "${phase}" == "Ready" ]]; then
            log_success "CMSC is Ready"
            return 0
        fi

        if [[ -z "${phase}" ]]; then
            empty_ticks=$((empty_ticks + 1))
            if (( empty_ticks >= 12 )); then
                log_error "CMSC status.phase stayed empty for ~2m — operator is not reconciling this CR"
                log_error "Check controller image/rollout and logs:"
                log_error "  oc get deploy -n ${OPERATOR_NS}"
                log_error "  oc logs -n ${OPERATOR_NS} deploy/koku-service-operator-controller-manager --tail=100"
                oc get cmsc "${CR_NAME}" -n "${NAMESPACE}" -o yaml || true
                oc get pods -n "${OPERATOR_NS}" -l control-plane=controller-manager || true
                exit 1
            fi
            log_info "phase=<empty> (waiting for operator to start reconciling; tick ${empty_ticks}/12)"
        else
            empty_ticks=0
            if [[ "${phase}" != "${last_phase}" ]] || [[ "${VERBOSE}" == "true" ]]; then
                if [[ -n "${msg}" ]]; then
                    log_info "phase=${phase} | ${msg}"
                else
                    log_info "phase=${phase}"
                fi
                last_phase="${phase}"
            else
                echo -n "."
            fi
        fi
        sleep 10
    done
    echo ""
    log_error "CMSC did not become Ready within ${READY_TIMEOUT}s"
    oc get cmsc "${CR_NAME}" -n "${NAMESPACE}" -o yaml || true
    oc describe cmsc "${CR_NAME}" -n "${NAMESPACE}" || true
    oc logs -n "${OPERATOR_NS}" deploy/koku-service-operator-controller-manager --tail=80 || true
    exit 1
}

main() {
    require_oc
    echo ""
    echo -e "${BLUE}Cost Management CMSC install${NC}"
    echo "=============================="
    echo "NAMESPACE=${NAMESPACE} CR_NAME=${CR_NAME} OPERATOR_NS=${OPERATOR_NS}"
    echo ""

    mirror_ui_oauth

    if [[ "${SKIP_DEPLOY}" == "true" ]]; then
        log_warning "Skipping CMSC apply and wait (--skip-deploy)"
        return 0
    fi

    apply_cmsc

    if [[ "${SKIP_WAIT}" == "true" ]]; then
        log_warning "Skipping wait for Ready (--skip-wait)"
        return 0
    fi

    wait_cmsc_ready
    log_success "CMSC install completed"
}

main "$@"
