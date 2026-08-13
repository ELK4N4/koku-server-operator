#!/bin/bash

# Download CI artifacts from OpenShift CI for debugging
#
# Usage:
#   ./download-ci-artifacts.sh <PR_NUMBER> <BUILD_ID> [OUTPUT_DIR]
#   ./download-ci-artifacts.sh --url <GCSWEB_URL> [OUTPUT_DIR]
#
# Examples:
#   ./download-ci-artifacts.sh 50 2014360404288868352
#   ./download-ci-artifacts.sh --url "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/pr-logs/pull/insights-onprem_cost-onprem-chart/50/pull-ci-insights-onprem-cost-onprem-chart-main-e2e/2014360404288868352/"
#
# Prerequisites:
#   - gcloud CLI installed (https://cloud.google.com/sdk/docs/install)
#   - Authenticated: gcloud auth login

set -euo pipefail

# Color codes
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
    cat << 'HELP'
Download CI artifacts from OpenShift CI

Usage:
  ./download-ci-artifacts.sh <PR_NUMBER> <BUILD_ID> [OUTPUT_DIR]
  ./download-ci-artifacts.sh --url <URL> [OUTPUT_DIR]

Arguments:
  PR_NUMBER     Pull request number (e.g., 50)
  BUILD_ID      CI build ID (e.g., 2014360404288868352)
  OUTPUT_DIR    Output directory (default: ./ci-artifacts-pr<PR>-<BUILD_ID>)

Options:
  --url         Parse PR and build ID from a Prow or gcsweb URL
  --help        Show this help message

Supported URL formats:
  - Prow:   https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/insights-onprem_cost-onprem-chart/<PR>/<JOB>/<BUILD_ID>
  - gcsweb: https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/pr-logs/pull/insights-onprem_cost-onprem-chart/<PR>/<JOB>/<BUILD_ID>/

Examples:
  # Download all artifacts for PR #50
  ./download-ci-artifacts.sh 50 2014360404288868352

  # Download from Prow URL (copy from GitHub CI check "Details" link)
  ./download-ci-artifacts.sh --url "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/insights-onprem_cost-onprem-chart/50/pull-ci-insights-onprem-cost-onprem-chart-main-e2e/2014360404288868352"

  # Download from gcsweb URL
  ./download-ci-artifacts.sh --url "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/pr-logs/pull/insights-onprem_cost-onprem-chart/50/pull-ci-insights-onprem-cost-onprem-chart-main-e2e/2014360404288868352/"

  # Download to specific directory
  ./download-ci-artifacts.sh 50 2014360404288868352 ./my-artifacts
HELP
    exit 0
}

# Check prerequisites
check_prerequisites() {
    if ! command -v gcloud &> /dev/null; then
        log_error "gcloud CLI not found. Install from: https://cloud.google.com/sdk/docs/install"
        exit 1
    fi
}

# Parse Prow or gcsweb URL to extract PR and build ID
parse_url() {
    local url="$1"
    
    # Extract from URL like:
    # Prow:   https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/insights-onprem_cost-onprem-chart/50/pull-ci-insights-onprem-cost-onprem-chart-main-e2e/2014360404288868352
    # gcsweb: https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/pr-logs/pull/insights-onprem_cost-onprem-chart/50/pull-ci-insights-onprem-cost-onprem-chart-main-e2e/2014360404288868352/
    
    if [[ "$url" =~ insights-onprem_cost-onprem-chart/([0-9]+)/([^/]+)/([0-9]+) ]]; then
        PR_NUMBER="${BASH_REMATCH[1]}"
        JOB_NAME="${BASH_REMATCH[2]}"
        BUILD_ID="${BASH_REMATCH[3]}"
    else
        log_error "Could not parse URL: $url"
        log_error "Expected format: .../insights-onprem_cost-onprem-chart/<PR>/<JOB>/<BUILD_ID>"
        log_error ""
        log_error "Supported URL formats:"
        log_error "  Prow:   https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/insights-onprem_cost-onprem-chart/<PR>/<JOB>/<BUILD_ID>"
        log_error "  gcsweb: https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/pr-logs/pull/insights-onprem_cost-onprem-chart/<PR>/<JOB>/<BUILD_ID>/"
        exit 1
    fi
}

# Main download function
download_artifacts() {
    local pr_number="$1"
    local build_id="$2"
    local output_dir="$3"
    local job_name="${JOB_NAME:-pull-ci-insights-onprem-cost-onprem-chart-main-e2e}"
    
    local gcs_path="gs://test-platform-results/pr-logs/pull/insights-onprem_cost-onprem-chart/${pr_number}/${job_name}/${build_id}/"
    
    echo ""
    echo "============================================================"
    echo "DOWNLOADING CI ARTIFACTS"
    echo "============================================================"
    echo "PR:        #${pr_number}"
    echo "Build ID:  ${build_id}"
    echo "Job:       ${job_name}"
    echo "GCS Path:  ${gcs_path}"
    echo "Output:    ${output_dir}"
    echo "============================================================"
    echo ""
    
    mkdir -p "${output_dir}"
    
    log_info "Downloading artifacts..."
    if gcloud storage cp -r "${gcs_path}" "${output_dir}/" 2>&1; then
        log_success "Download complete!"
    else
        log_warning "gcloud storage failed, trying gsutil..."
        gsutil -m cp -r "${gcs_path}" "${output_dir}/"
    fi
    
    echo ""
    log_info "Downloaded files:"
    find "${output_dir}" -type f -exec ls -lh {} \; 2>/dev/null | head -20
    
    # Show key files
    echo ""
    log_info "Key files:"
    [[ -f "${output_dir}/build-log.txt" ]] && echo "  - build-log.txt (main CI log)"
    [[ -f "${output_dir}/finished.json" ]] && echo "  - finished.json (job result)"
    [[ -d "${output_dir}/artifacts" ]] && echo "  - artifacts/ (step artifacts)"
    
    # Check for JUnit reports
    local junit_files
    junit_files=$(find "${output_dir}" -name "*.xml" -path "*junit*" 2>/dev/null || true)
    if [[ -n "$junit_files" ]]; then
        echo ""
        log_info "JUnit reports found:"
        echo "$junit_files" | while read -r f; do echo "  - $f"; done
    fi
    
    echo ""
    log_success "Artifacts downloaded to: ${output_dir}"
}

# Parse arguments
main() {
    local pr_number=""
    local build_id=""
    local output_dir=""
    local url=""
    
    while [[ $# -gt 0 ]]; do
        case $1 in
            --url)
                url="$2"
                shift 2
                ;;
            --help|-h)
                show_help
                ;;
            *)
                if [[ -z "$pr_number" ]]; then
                    pr_number="$1"
                elif [[ -z "$build_id" ]]; then
                    build_id="$1"
                elif [[ -z "$output_dir" ]]; then
                    output_dir="$1"
                fi
                shift
                ;;
        esac
    done
    
    check_prerequisites
    
    # Parse URL if provided
    if [[ -n "$url" ]]; then
        parse_url "$url"
        output_dir="${output_dir:-./ci-artifacts-pr${PR_NUMBER}-${BUILD_ID}}"
        download_artifacts "$PR_NUMBER" "$BUILD_ID" "$output_dir"
    elif [[ -n "$pr_number" && -n "$build_id" ]]; then
        output_dir="${output_dir:-./ci-artifacts-pr${pr_number}-${build_id}}"
        download_artifacts "$pr_number" "$build_id" "$output_dir"
    else
        log_error "Missing required arguments"
        echo ""
        show_help
    fi
}

main "$@"
