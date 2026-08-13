#!/bin/bash

# Run pytest suite against an operator-deployed Cost Management stack.
#
# Usage:
#   ./scripts/run-pytest.sh [OPTIONS] [PYTEST_ARGS...]
#
# Suite Options:
#   --auth              JWT authentication tests
#   --infrastructure    Infrastructure health (DB, S3, Kafka)
#   --cost-management   Cost Management (Koku) pipeline tests
#   --sources           Sources API tests
#   --ros               ROS/Kruize recommendation tests
#   --e2e               End-to-end tests
#   --ui                UI tests only (Playwright)
#   --no-ui             Exclude UI tests
#   --api               API suite
#   --interpod          Inter-pod suite
#   --performance       Performance tests (excluded by default)
#   --perf-*            Individual performance suites
#
# Filter Options:
#   --smoke             Smoke tests only
#   --slow              Include slow tests
#
# Setup Options:
#   --setup-only        Only setup venv / deps
#   --no-venv           Use system Python
#   --help              Show help
#
# Environment Variables:
#   NAMESPACE              Target namespace (default: cost-onprem)
#   CR_NAME                CostManagementServiceConfig name (default: cost-management)
#   KEYCLOAK_NAMESPACE     Keycloak namespace (default: keycloak)
#   PYTHON                 Python interpreter (default: auto — prefers 3.11, then 3.12/3.10)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TESTS_DIR="${PROJECT_ROOT}/test/pytest"
VENV_DIR="${TESTS_DIR}/.venv"
REPORTS_DIR="${TESTS_DIR}/reports"

# CI uses Python 3.11; avoid bleeding-edge interpreters (e.g. 3.14) that break pip/deps.
PYTHON_MIN_MINOR=10
PYTHON_MAX_MINOR=12
PYTHON_AUTO_RESOLVED=false
USE_VENV=true
SETUP_ONLY=false

export NAMESPACE="${NAMESPACE:-cost-onprem}"
export CR_NAME="${CR_NAME:-cost-management}"
export KEYCLOAK_NAMESPACE="${KEYCLOAK_NAMESPACE:-keycloak}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $*"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

show_help() {
    sed -n '/^# Run pytest/,/^set -e$/p' "$0" | grep '^#' | sed 's/^# \?//'
    exit 0
}

python_version_tuple() {
    local py="$1"
    "$py" -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")'
}

python_version_ok() {
    local py="$1"
    "$py" -c "import sys; raise SystemExit(0 if (${PYTHON_MIN_MINOR} <= sys.version_info.minor <= ${PYTHON_MAX_MINOR}) else 1)" 2>/dev/null
}

resolve_python() {
    if [[ -n "${PYTHON:-}" ]]; then
        return 0
    fi
    local candidate
    for candidate in python3.11 python3.12 python3.10 python3; do
        if command -v "$candidate" >/dev/null 2>&1 && python_version_ok "$candidate"; then
            PYTHON="$candidate"
            PYTHON_AUTO_RESOLVED=true
            return 0
        fi
    done
    PYTHON=python3
}

venv_python() {
    echo "${VENV_DIR}/bin/python"
}

venv_python_matches_selected() {
    local vpy
    vpy="$(venv_python)"
    [[ -x "$vpy" ]] || return 1
    local venv_ver selected_ver
    venv_ver="$("$vpy" -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')"
    selected_ver="$(python_version_tuple "$PYTHON")"
    [[ "$venv_ver" == "$selected_ver" ]]
}

venv_pip_works() {
    local vpy="$1"
    "$vpy" -m pip --version >/dev/null 2>&1
}

create_venv() {
    rm -rf "$VENV_DIR"
    log_info "Creating virtual environment at ${VENV_DIR} with ${PYTHON}"
    "$PYTHON" -m venv --copies "$VENV_DIR" 2>/dev/null || "$PYTHON" -m venv "$VENV_DIR"
}

install_venv_dependencies() {
    local vpy
    vpy="$(venv_python)"
    if ! venv_pip_works "$vpy"; then
        log_error "pip is not usable in ${VENV_DIR} (interpreter: ${PYTHON})"
        return 1
    fi
    # shellcheck disable=SC1091
    source "${VENV_DIR}/bin/activate"
    export PYTHONNOUSERSITE=1
    "$vpy" -m pip install --upgrade pip >/dev/null
    "$vpy" -m pip install -r "${TESTS_DIR}/requirements.txt"
}

check_prerequisites() {
    log_info "Checking prerequisites..."
    resolve_python
    if ! command -v "$PYTHON" &> /dev/null; then
        log_error "Python not found: $PYTHON"
        exit 1
    fi
    if ! python_version_ok "$PYTHON"; then
        local ver
        ver="$(python_version_tuple "$PYTHON")"
        log_error "Unsupported Python ${ver} for pytest (need 3.${PYTHON_MIN_MINOR}–3.${PYTHON_MAX_MINOR}, CI uses 3.11)"
        log_error "Install python3.11 or run: PYTHON=python3.11 ./scripts/run-pytest.sh"
        exit 1
    fi
    if [[ "$PYTHON_AUTO_RESOLVED" == "true" ]]; then
        log_info "Auto-selected ${PYTHON} ($(python_version_tuple "$PYTHON"))"
    else
        log_info "Using ${PYTHON} ($(python_version_tuple "$PYTHON"))"
    fi
    if ! command -v oc &> /dev/null; then
        log_error "oc CLI not found."
        exit 1
    fi
    if ! oc whoami &> /dev/null; then
        log_error "Not logged into OpenShift. Run 'oc login' first."
        exit 1
    fi
    log_info "Logged in as: $(oc whoami)"
}

setup_venv() {
    if [[ "$USE_VENV" != "true" ]]; then
        log_warning "Skipping venv (--no-venv)"
        return 0
    fi
    log_info "Setting up virtual environment at ${VENV_DIR}"

    local vpy recreate=false
    if [[ -d "$VENV_DIR" ]]; then
        if ! venv_python_matches_selected; then
            log_warning "Existing venv uses a different Python version; recreating"
            recreate=true
        elif ! venv_pip_works "$(venv_python)"; then
            log_warning "Existing venv has a broken pip; recreating"
            recreate=true
        fi
    else
        recreate=true
    fi

    if [[ "$recreate" == "true" ]]; then
        create_venv
    fi

    vpy="$(venv_python)"
    if [[ ! -x "$vpy" ]]; then
        log_error "venv python missing: $vpy"
        exit 1
    fi
    # Refuse a broken venv that still reports system prefix.
    if ! "$vpy" -c 'import sys; raise SystemExit(0 if sys.prefix != sys.base_prefix else 1)' 2>/dev/null; then
        log_warning "venv looks broken (sys.prefix==base); recreating with --copies"
        create_venv
        vpy="$(venv_python)"
    fi

    if ! install_venv_dependencies; then
        log_warning "pip install failed; recreating venv once and retrying"
        create_venv
        install_venv_dependencies || {
            log_error "Failed to install pytest dependencies after venv recreate"
            exit 1
        }
    fi
    log_success "Dependencies installed"
}

install_playwright_browsers() {
    log_info "Installing Playwright browsers..."
    if command -v playwright &> /dev/null || python -c "import playwright" 2>/dev/null; then
        python -m playwright install --with-deps chromium || python -m playwright install chromium
    fi
}

setup_reports_dir() {
    mkdir -p "$REPORTS_DIR"
}

run_pytest() {
    local pytest_args=("$@")
    cd "$TESTS_DIR"

    local py="python"
    if [[ "$USE_VENV" == "true" && -x "${VENV_DIR}/bin/python" ]]; then
        py="${VENV_DIR}/bin/python"
        # shellcheck disable=SC1091
        source "${VENV_DIR}/bin/activate"
        export PYTHONNOUSERSITE=1
    fi

    local html_args=()
    if "$py" -c "import pytest_html" 2>/dev/null; then
        if [[ -n "${PERF_REPORTS_DIR:-}" ]]; then
            html_args+=("--html=${PERF_REPORTS_DIR}/report.html" "--self-contained-html")
        else
            html_args+=("--html=${REPORTS_DIR}/report.html" "--self-contained-html")
        fi
    fi

    echo ""
    echo "============================================================"
    echo "Working directory: $(pwd)"
    echo "Python: $("$py" -c 'import sys; print(sys.executable)')"
    echo "NAMESPACE=${NAMESPACE}"
    echo "CR_NAME=${CR_NAME}"
    echo "KEYCLOAK_NAMESPACE=${KEYCLOAK_NAMESPACE}"
    echo "============================================================"
    echo ""

    local exit_code=0
    "$py" -m pytest "${html_args[@]}" "${pytest_args[@]}" || exit_code=$?
    return $exit_code
}

main() {
    local pytest_markers=()
    local pytest_extra_args=()
    local include_ui=true
    local exclude_ui=false
    local ui_only=false

    while [[ $# -gt 0 ]]; do
        case $1 in
            --auth) pytest_markers+=("auth"); shift ;;
            --infrastructure) pytest_markers+=("infrastructure"); shift ;;
            --cost-management) pytest_markers+=("cost_management"); shift ;;
            --sources) pytest_markers+=("sources"); shift ;;
            --ros) pytest_markers+=("ros"); shift ;;
            --e2e) pytest_markers+=("e2e"); shift ;;
            --api) pytest_markers+=("api"); shift ;;
            --interpod) pytest_markers+=("interpod"); shift ;;
            --ui) ui_only=true; shift ;;
            --no-ui) exclude_ui=true; include_ui=false; shift ;;
            --performance)
                pytest_markers+=("performance"); include_ui=false; shift ;;
            --perf-ingestion)
                pytest_markers+=("performance and ingestion"); include_ui=false; shift ;;
            --perf-api)
                pytest_markers+=("performance and api_latency"); include_ui=false; shift ;;
            --perf-scale)
                pytest_markers+=("performance and scale"); include_ui=false; shift ;;
            --perf-ros)
                pytest_markers+=("performance and ros_perf"); include_ui=false; shift ;;
            --perf-soak)
                pytest_markers+=("performance and soak"); include_ui=false; shift ;;
            --perf-valkey)
                pytest_markers+=("performance and valkey_eviction"); include_ui=false; shift ;;
            --perf-db)
                pytest_markers+=("performance and db_sweep"); include_ui=false; shift ;;
            --perf-kafka)
                pytest_markers+=("performance and kafka_throughput"); include_ui=false; shift ;;
            --perf-celery)
                pytest_markers+=("performance and celery_scaling"); include_ui=false; shift ;;
            --perf-stress)
                pytest_markers+=("performance and stress"); include_ui=false; shift ;;
            --perf-stress-ramp)
                pytest_markers+=("performance and stress_ramp"); include_ui=false; shift ;;
            --perf-stress-recovery)
                pytest_markers+=("performance and stress_recovery"); include_ui=false; shift ;;
            --smoke) pytest_markers+=("smoke"); shift ;;
            --slow) pytest_markers+=("slow"); shift ;;
            --setup-only) SETUP_ONLY=true; shift ;;
            --no-venv) USE_VENV=false; shift ;;
            --help|-h) show_help ;;
            -m)
                if [[ "$2" == *"not ui"* ]]; then
                    include_ui=false; exclude_ui=true
                elif [[ "$2" == "ui" ]]; then
                    ui_only=true
                fi
                pytest_extra_args+=("$1" "$2"); shift 2 ;;
            *)
                pytest_extra_args+=("$1"); shift ;;
        esac
    done

    echo ""
    echo -e "${BLUE}Cost Management Operator Pytest Suite${NC}"
    echo "======================================"
    echo ""

    check_prerequisites
    setup_venv

    if [[ "$include_ui" == "true" ]] || [[ "$ui_only" == "true" ]]; then
        install_playwright_browsers
    fi

    setup_reports_dir

    if [[ "$SETUP_ONLY" == "true" ]]; then
        log_success "Environment setup complete"
        exit 0
    fi

    local pytest_args=()
    local is_perf_test=false
    local perf_reports_dir=""

    if [[ "$ui_only" == "true" ]]; then
        pytest_args+=("-m" "ui")
    elif [[ ${#pytest_markers[@]} -gt 0 ]]; then
        local marker_expr=""
        for _m in "${pytest_markers[@]}"; do
            if [[ -n "$marker_expr" ]]; then
                marker_expr="($marker_expr) or ($_m)"
            else
                marker_expr="$_m"
            fi
        done
        if [[ "$marker_expr" == *"performance"* ]]; then
            pytest_args+=("-m" "$marker_expr")
            is_perf_test=true
        else
            pytest_args+=("-m" "($marker_expr) and not performance")
        fi
    elif [[ "$exclude_ui" == "true" ]]; then
        pytest_args+=("-m" "not ui and not performance")
    else
        # Default: all suites except performance (UI included)
        pytest_args+=("-m" "not performance")
    fi

    if [[ "$is_perf_test" == "true" ]] && [[ -n "${PERF_OUTPUT_DIR:-}" ]] && [[ -n "${TEST_RUN_ID:-}" ]]; then
        local perf_output_dir_resolved="${PERF_OUTPUT_DIR}"
        if [[ "$perf_output_dir_resolved" != /* ]]; then
            perf_output_dir_resolved="${PROJECT_ROOT}/${PERF_OUTPUT_DIR}"
        fi
        perf_reports_dir="${perf_output_dir_resolved}/${TEST_RUN_ID}/reports"
        mkdir -p "$perf_reports_dir"
        pytest_args+=("--junit-xml=${perf_reports_dir}/junit.xml")
        export PERF_REPORTS_DIR="$perf_reports_dir"
    fi

    if [[ ${#pytest_extra_args[@]} -gt 0 ]]; then
        pytest_args+=("${pytest_extra_args[@]}")
    fi

    local exit_code=0
    run_pytest "${pytest_args[@]}" || exit_code=$?

    echo ""
    if [[ $exit_code -eq 0 ]]; then
        log_success "All tests passed!"
    else
        log_error "Some tests failed (exit code: $exit_code)"
    fi

    if [[ -n "$perf_reports_dir" ]]; then
        log_info "JUnit report: ${perf_reports_dir}/junit.xml"
    else
        log_info "Reports: ${REPORTS_DIR}/"
    fi

    exit $exit_code
}

main "$@"
