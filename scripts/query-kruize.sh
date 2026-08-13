#!/bin/bash

# Script to query Kruize for experiments and recommendations
# Can be used to check results after uploads or test executions

set -eo pipefail

# Default values
NAMESPACE="${NAMESPACE:-cost-onprem}"

# Extract database credentials from secret
DB_SECRET_NAME="${DB_SECRET_NAME:-cost-onprem-db-credentials}"
KRUIZE_USER=$(oc get secret -n "$NAMESPACE" "$DB_SECRET_NAME" -o jsonpath='{.data.kruize-user}' 2>/dev/null | base64 -d)
KRUIZE_PASSWORD=$(oc get secret -n "$NAMESPACE" "$DB_SECRET_NAME" -o jsonpath='{.data.kruize-password}' 2>/dev/null | base64 -d)
KRUIZE_DB="kruize_db"

if [ -z "$KRUIZE_USER" ] || [ -z "$KRUIZE_PASSWORD" ]; then
    echo "Error: Unable to retrieve Kruize database credentials from secret '$DB_SECRET_NAME'" >&2
    echo "Ensure the secret exists and contains 'kruize-user' and 'kruize-password' keys" >&2
    exit 1
fi

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions for colored output
echo_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

echo_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

echo_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

echo_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

# Function to check if we can access Kruize database
check_kruize_access() {
    local db_pod=$(oc get pods -n "$NAMESPACE" -l "app.kubernetes.io/component=database" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

    if [ -z "$db_pod" ]; then
        echo_error "Kruize database pod not found in namespace: $NAMESPACE"
        echo_info "Check if Kruize is deployed: oc get pods -n $NAMESPACE | grep kruize"
        return 1
    fi

    echo "$db_pod"
}

# Function to list all experiments
list_experiments() {
    echo_info "=== Kruize Experiments ==="
    echo ""

    local db_pod=$(check_kruize_access) || return 1

    local count=$(oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -t -c "SELECT COUNT(*) FROM kruize_experiments;" 2>/dev/null | tr -d ' ' || echo "0")

    if [ "$count" -eq 0 ]; then
        echo_warning "No experiments found in database"
        echo_info "This means no data has been processed by Kruize yet"
        return 0
    fi

    echo_success "Found $count experiment(s)"
    echo ""

    echo_info "Experiment details:"
    oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -c \
        "SELECT experiment_id, experiment_name, cluster_name, status, mode
         FROM kruize_experiments
         ORDER BY experiment_id DESC
         LIMIT 20;" 2>/dev/null || echo_error "Failed to query experiments"
}

# Function to list all recommendations
list_recommendations() {
    echo_info "=== Kruize Recommendations ==="
    echo ""

    local db_pod=$(check_kruize_access) || return 1

    local count=$(oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -t -c "SELECT COUNT(*) FROM kruize_recommendations;" 2>/dev/null | tr -d ' ' || echo "0")

    if [ "$count" -eq 0 ]; then
        echo_warning "No recommendations found in database"
        echo_info "Possible reasons:"
        echo_info "  - Not enough data points collected yet (Kruize needs multiple intervals)"
        echo_info "  - Experiments exist but are still being analyzed"
        echo_info "  - Data quality issues preventing recommendation generation"
        return 0
    fi

    echo_success "Found $count recommendation(s)"
    echo ""

    echo_info "Recent recommendations (short_term cost recommendations):"
    oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -c \
        "SELECT
            experiment_name,
            interval_end_time,
            extended_data->'kubernetes_objects'->0->'containers'->0->'recommendations'->'data'->to_char(interval_end_time, 'YYYY-MM-DD\"T\"HH24:MI:SS\".000Z\"')->'recommendation_terms'->'short_term'->'recommendation_engines'->'cost'->'config'->'requests'->'cpu'->>'amount' || ' cores' as cpu_request_rec,
            extended_data->'kubernetes_objects'->0->'containers'->0->'recommendations'->'data'->to_char(interval_end_time, 'YYYY-MM-DD\"T\"HH24:MI:SS\".000Z\"')->'recommendation_terms'->'short_term'->'recommendation_engines'->'cost'->'config'->'limits'->'cpu'->>'amount' || ' cores' as cpu_limit_rec,
            extended_data->'kubernetes_objects'->0->'containers'->0->'recommendations'->'data'->to_char(interval_end_time, 'YYYY-MM-DD\"T\"HH24:MI:SS\".000Z\"')->'recommendation_terms'->'short_term'->'recommendation_engines'->'cost'->'config'->'requests'->'memory'->>'amount' || ' bytes' as memory_request_rec,
            extended_data->'kubernetes_objects'->0->'containers'->0->'recommendations'->'data'->to_char(interval_end_time, 'YYYY-MM-DD\"T\"HH24:MI:SS\".000Z\"')->'recommendation_terms'->'short_term'->'recommendation_engines'->'cost'->'config'->'limits'->'memory'->>'amount' || ' bytes' as memory_limit_rec
         FROM kruize_recommendations
         ORDER BY interval_end_time DESC
         LIMIT 10;" 2>/dev/null || echo_error "Failed to query recommendations"
}

# Function to query by experiment name/pattern
query_by_experiment() {
    local experiment_pattern="$1"

    if [ -z "$experiment_pattern" ]; then
        echo_error "Experiment name/pattern is required"
        echo_info "Usage: $0 --experiment <name_or_pattern>"
        return 1
    fi

    echo_info "=== Query by Experiment: $experiment_pattern ==="
    echo ""

    local db_pod=$(check_kruize_access) || return 1

    echo_info "Matching experiments:"
    oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -c \
        "SELECT experiment_id, experiment_name, cluster_name, status
         FROM kruize_experiments
         WHERE experiment_name LIKE '%${experiment_pattern}%'
         ORDER BY experiment_id DESC;" 2>/dev/null || echo_error "Failed to query experiments"

    echo ""
    echo_info "Recommendations for matching experiments:"
    oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -c \
        "SELECT r.experiment_name, r.interval_end_time, r.cluster_name
         FROM kruize_recommendations r
         WHERE r.experiment_name LIKE '%${experiment_pattern}%'
         ORDER BY r.interval_end_time DESC
         LIMIT 20;" 2>/dev/null || echo_error "Failed to query recommendations"
}

# Function to query by cluster
query_by_cluster() {
    local cluster_id="$1"

    if [ -z "$cluster_id" ]; then
        echo_error "Cluster ID is required"
        echo_info "Usage: $0 --cluster <cluster_id>"
        return 1
    fi

    echo_info "=== Query by Cluster: $cluster_id ==="
    echo ""

    local db_pod=$(check_kruize_access) || return 1

    echo_info "Experiments for cluster:"
    oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -c \
        "SELECT experiment_id, experiment_name, status, mode
         FROM kruize_experiments
         WHERE cluster_name LIKE '%${cluster_id}%'
         ORDER BY experiment_id DESC;" 2>/dev/null || echo_error "Failed to query experiments"

    echo ""
    echo_info "Recommendations for cluster (short_term cost recommendations):"
    oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -c \
        "SELECT
            experiment_name,
            interval_end_time,
            extended_data->'kubernetes_objects'->0->'containers'->0->'recommendations'->'data'->to_char(interval_end_time, 'YYYY-MM-DD\"T\"HH24:MI:SS\".000Z\"')->'recommendation_terms'->'short_term'->'recommendation_engines'->'cost'->'config'->'requests'->'cpu'->>'amount' || ' cores' as cpu_request_rec,
            extended_data->'kubernetes_objects'->0->'containers'->0->'recommendations'->'data'->to_char(interval_end_time, 'YYYY-MM-DD\"T\"HH24:MI:SS\".000Z\"')->'recommendation_terms'->'short_term'->'recommendation_engines'->'cost'->'config'->'limits'->'cpu'->>'amount' || ' cores' as cpu_limit_rec,
            extended_data->'kubernetes_objects'->0->'containers'->0->'recommendations'->'data'->to_char(interval_end_time, 'YYYY-MM-DD\"T\"HH24:MI:SS\".000Z\"')->'recommendation_terms'->'short_term'->'recommendation_engines'->'cost'->'config'->'requests'->'memory'->>'amount' || ' bytes' as memory_request_rec,
            extended_data->'kubernetes_objects'->0->'containers'->0->'recommendations'->'data'->to_char(interval_end_time, 'YYYY-MM-DD\"T\"HH24:MI:SS\".000Z\"')->'recommendation_terms'->'short_term'->'recommendation_engines'->'cost'->'config'->'limits'->'memory'->>'amount' || ' bytes' as memory_limit_rec
         FROM kruize_recommendations
         WHERE cluster_name LIKE '%${cluster_id}%'
         ORDER BY interval_end_time DESC
         LIMIT 5;" 2>/dev/null || echo_error "Failed to query recommendations"

    echo ""
    local rec_count=$(oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -t -c \
        "SELECT COUNT(*)
         FROM kruize_recommendations
         WHERE cluster_name LIKE '%${cluster_id}%';" 2>/dev/null | tr -d ' ' || echo "0")

    echo_success "Found $rec_count recommendation(s) for cluster $cluster_id"
}

# Function to get detailed recommendation info
get_recommendation_details() {
    local experiment_name="$1"

    if [ -z "$experiment_name" ]; then
        echo_error "Experiment name is required"
        echo_info "Usage: $0 --detail <experiment_name>"
        return 1
    fi

    echo_info "=== Recommendation Details: $experiment_name ==="
    echo ""

    local db_pod=$(check_kruize_access) || return 1

    echo_info "Latest recommendations for experiment:"
    oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -x -c \
        "SELECT * FROM kruize_recommendations WHERE experiment_name = '${experiment_name}' ORDER BY interval_end_time DESC LIMIT 5;" 2>/dev/null || echo_error "Failed to query recommendation"
}

# Function to run custom SQL query
run_custom_query() {
    local query="$1"

    if [ -z "$query" ]; then
        echo_error "SQL query is required"
        echo_info "Usage: $0 --query '<SQL_QUERY>'"
        return 1
    fi

    echo_info "=== Running Custom Query ==="
    echo_info "Query: $query"
    echo ""

    local db_pod=$(check_kruize_access) || return 1

    oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -c "$query" 2>/dev/null || echo_error "Query failed"
}

# Function to show schema
show_schema() {
    echo_info "=== Kruize Database Schema ==="
    echo ""

    local db_pod=$(check_kruize_access) || return 1

    echo_info "Tables in database:"
    oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -c "\dt" 2>/dev/null || echo_error "Failed to list tables"

    echo ""
    echo_info "Experiments table structure:"
    oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -c "\d kruize_experiments" 2>/dev/null || true

    echo ""
    echo_info "Recommendations table structure:"
    oc exec -n "$NAMESPACE" "$db_pod" -- \
        env PGPASSWORD="$KRUIZE_PASSWORD" psql -U "$KRUIZE_USER" -d "$KRUIZE_DB" -c "\d kruize_recommendations" 2>/dev/null || true
}

# Help function
show_help() {
    cat << EOF
Usage: $0 [OPTION]

Query Kruize database for experiments and recommendations

OPTIONS:
  --experiments, -e              List all experiments
  --recommendations, -r          List all recommendations
  --experiment <pattern>, -x     Query by experiment name pattern
  --cluster <cluster_id>, -c     Query by cluster ID
  --detail <exp_name>, -d        Get detailed recommendations for an experiment
  --query <sql>, -q              Run custom SQL query
  --schema, -s                   Show database schema
  --namespace <ns>, -n           Target namespace (default: $NAMESPACE)
  --help, -h                     Show this help message

EXAMPLES:
  # List all experiments
  $0 --experiments

  # List all recommendations
  $0 --recommendations

  # Find experiments matching a pattern
  $0 --experiment "test-cluster"

  # Query by cluster ID
  $0 --cluster "757b6bf6-9e91-486a-8a99-6d3e6d0f485c"

  # Get detailed recommendations for an experiment
  $0 --detail "1|757b6bf6-9e91-486a-8a99-6d3e6d0f485c|757b6bf6-9e91-486a-8a99-6d3e6d0f485c|cost-onprem|deployment|ros-kruize"

  # Run custom query
  $0 --query "SELECT COUNT(*) FROM kruize_experiments WHERE status='IN_PROGRESS';"

  # Show database schema
  $0 --schema

DATABASE ACCESS:
  This script connects to the Kruize PostgreSQL database using dedicated Kruize credentials.

  Database Configuration:
    - Database Name: kruize_db (on unified database server)
    - User Credentials: Retrieved from secret {{ release-name }}-db-credentials
    - Secret Keys: kruize-user, kruize-password
    - Database Pod: Identified by label app.kubernetes.io/component=database

  Available tables:
    - kruize_experiments: Experiment definitions and status
    - kruize_recommendations: Generated recommendations

  SQL Query Examples:
    - Count experiments: SELECT COUNT(*) FROM kruize_experiments;
    - Recent recommendations: SELECT * FROM kruize_recommendations ORDER BY id DESC LIMIT 10;
    - Experiments by namespace: SELECT * FROM kruize_experiments WHERE namespace='cost-onprem';

EOF
}

# Main function
main() {
    case "${1:-}" in
        --experiments|-e)
            list_experiments
            ;;
        --recommendations|-r)
            list_recommendations
            ;;
        --experiment|-x)
            query_by_experiment "$2"
            ;;
        --cluster|-c)
            query_by_cluster "$2"
            ;;
        --detail|-d)
            get_recommendation_details "$2"
            ;;
        --query|-q)
            run_custom_query "$2"
            ;;
        --schema|-s)
            show_schema
            ;;
        --namespace|-n)
            NAMESPACE="$2"
            main "${3:-}" "${4:-}"
            ;;
        --help|-h|"")
            show_help
            ;;
        *)
            echo_error "Unknown option: $1"
            echo_info "Use '$0 --help' for usage information"
            exit 1
            ;;
    esac
}

main "$@"


