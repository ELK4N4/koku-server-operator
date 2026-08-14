#!/bin/bash
# Script to get a JWT token that is valid for Envoy
# This token can be used to authenticate requests to the Envoy proxy

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo_info() { echo -e "${BLUE}ℹ${NC} $1"; }
echo_success() { echo -e "${GREEN}✓${NC} $1"; }
echo_warning() { echo -e "${YELLOW}⚠${NC} $1"; }
echo_error() { echo -e "${RED}✗${NC} $1"; }

# Default values
KEYCLOAK_NAMESPACE="${KEYCLOAK_NAMESPACE:-keycloak}"
REALM="${REALM:-kubernetes}"
CLIENT_ID="${CLIENT_ID:-cost-management-operator}"

echo_info "=== Getting JWT Token for Envoy ==="
echo ""

# Step 1: Detect Keycloak URL
echo_info "Step 1: Detecting Keycloak URL..."

KEYCLOAK_URL=""
keycloak_route=$(oc get route -n "$KEYCLOAK_NAMESPACE" keycloak -o jsonpath='{.spec.host}' 2>/dev/null || echo "")

if [ -n "$keycloak_route" ]; then
    KEYCLOAK_URL="https://$keycloak_route"
    echo_success "Found Keycloak route: $KEYCLOAK_URL"
else
    echo_warning "Keycloak route not found, trying service discovery..."
    keycloak_service=$(oc get svc -n "$KEYCLOAK_NAMESPACE" keycloak -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")
    if [ -n "$keycloak_service" ]; then
        KEYCLOAK_URL="http://keycloak.$KEYCLOAK_NAMESPACE.svc.cluster.local:8080"
        echo_success "Found Keycloak service: $KEYCLOAK_URL"
    else
        echo_error "Keycloak not found in namespace '$KEYCLOAK_NAMESPACE'"
        echo_info "Please ensure Keycloak is deployed or set KEYCLOAK_NAMESPACE environment variable"
        exit 1
    fi
fi

# Step 2: Get Client Secret
echo_info "Step 2: Getting client secret for '$CLIENT_ID'..."

CLIENT_SECRET=""

# Try to find KeycloakClient CR first
keycloak_clients=$(oc get keycloakclient -n "$KEYCLOAK_NAMESPACE" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
keycloak_client_cr=""

for client in $keycloak_clients; do
    if echo "$client" | grep -q "cost-management"; then
        keycloak_client_cr="$client"
        echo_info "Found KeycloakClient CR: $keycloak_client_cr"
        
        # Get client ID from CR
        detected_client_id=$(oc get keycloakclient "$keycloak_client_cr" -n "$KEYCLOAK_NAMESPACE" -o jsonpath='{.spec.client.clientId}' 2>/dev/null || echo "")
        if [ -n "$detected_client_id" ]; then
            CLIENT_ID="$detected_client_id"
            echo_info "Using client ID from CR: $CLIENT_ID"
        fi
        
        # Get secret from CR-generated secret
        secret_name="keycloak-client-secret-$keycloak_client_cr"
        CLIENT_SECRET=$(oc get secret "$secret_name" -n "$KEYCLOAK_NAMESPACE" -o jsonpath='{.data.CLIENT_SECRET}' 2>/dev/null | base64 -d || echo "")
        if [ -n "$CLIENT_SECRET" ]; then
            echo_success "Found client secret in: $secret_name"
            break
        fi
    fi
done

# If still no secret, try alternative patterns
if [ -z "$CLIENT_SECRET" ]; then
    echo_warning "Trying alternative secret name patterns..."
    for secret_pattern in \
        "keycloak-client-secret-cost-management-operator" \
        "keycloak-client-secret-cost-management-service-account" \
        "credential-$CLIENT_ID" \
        "keycloak-client-$CLIENT_ID" \
        "$CLIENT_ID-secret"; do
        CLIENT_SECRET=$(oc get secret "$secret_pattern" -n "$KEYCLOAK_NAMESPACE" -o jsonpath='{.data.CLIENT_SECRET}' 2>/dev/null | base64 -d || echo "")
        if [ -n "$CLIENT_SECRET" ]; then
            echo_success "Found client secret in: $secret_pattern"
            break
        fi
    done
fi

if [ -z "$CLIENT_SECRET" ]; then
    echo_error "Client secret not found for client '$CLIENT_ID'"
    echo_info "Available secrets in $KEYCLOAK_NAMESPACE namespace:"
    oc get secrets -n "$KEYCLOAK_NAMESPACE" | grep -E "(keycloak|client|secret)" || echo "No matching secrets found"
    exit 1
fi

# Step 3: Get JWT Token
echo_info "Step 3: Requesting JWT token from Keycloak..."

# Determine token endpoint (RHBK v22+ does not use /auth prefix)
# Try with /auth first, then without
token_url_with_auth="$KEYCLOAK_URL/auth/realms/$REALM/protocol/openid-connect/token"
token_url_no_auth="$KEYCLOAK_URL/realms/$REALM/protocol/openid-connect/token"

echo_info "Trying token endpoint: $token_url_no_auth"
echo_info "Client ID: $CLIENT_ID"

# Request JWT token using client credentials flow
token_response=$(curl -s -k -X POST "$token_url_no_auth" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=client_credentials" \
    -d "client_id=$CLIENT_ID" \
    -d "client_secret=$CLIENT_SECRET" 2>/dev/null)

# If that fails, try with /auth prefix
if echo "$token_response" | grep -q "404\|Not Found" || [ -z "$token_response" ]; then
    echo_warning "Trying with /auth prefix..."
    token_response=$(curl -s -k -X POST "$token_url_with_auth" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "grant_type=client_credentials" \
        -d "client_id=$CLIENT_ID" \
        -d "client_secret=$CLIENT_SECRET" 2>/dev/null)
fi

# Extract access token
if command -v jq >/dev/null 2>&1; then
    JWT_TOKEN=$(echo "$token_response" | jq -r '.access_token // empty' 2>/dev/null)
    expires_in=$(echo "$token_response" | jq -r '.expires_in // 0' 2>/dev/null)
    error=$(echo "$token_response" | jq -r '.error // empty' 2>/dev/null)
    error_description=$(echo "$token_response" | jq -r '.error_description // empty' 2>/dev/null)
elif command -v python3 >/dev/null 2>&1; then
    JWT_TOKEN=$(echo "$token_response" | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('access_token', ''))" 2>/dev/null)
    expires_in=$(echo "$token_response" | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('expires_in', 0))" 2>/dev/null)
    error=$(echo "$token_response" | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('error', ''))" 2>/dev/null)
    error_description=$(echo "$token_response" | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('error_description', ''))" 2>/dev/null)
else
    echo_error "Neither jq nor python3 is available. Please install one of them."
    exit 1
fi

if [ -n "$error" ]; then
    echo_error "Failed to get JWT token: $error"
    if [ -n "$error_description" ]; then
        echo_error "Error description: $error_description"
    fi
    echo_info "Token response: $token_response"
    exit 1
fi

if [ -z "$JWT_TOKEN" ] || [ "$JWT_TOKEN" = "null" ] || [ "$JWT_TOKEN" = "" ]; then
    echo_error "Failed to extract JWT token from response"
    echo_info "Token response: $token_response"
    exit 1
fi

echo_success "JWT token obtained successfully!"
echo ""

# Step 4: Display token information
echo_info "Token Information:"
echo "  Token length: ${#JWT_TOKEN} characters"
echo "  Expires in: ${expires_in:-300} seconds"

# Decode and display token claims (if jq is available)
if command -v jq >/dev/null 2>&1; then
    echo ""
    echo_info "Token Claims:"
    payload=$(echo "$JWT_TOKEN" | cut -d'.' -f2)
    # Add padding if needed
    padding=$((4 - ${#payload} % 4))
    if [ $padding -ne 4 ]; then
        payload="${payload}$(printf '%*s' $padding | tr ' ' '=')"
    fi
    decoded=$(echo "$payload" | base64 -d 2>/dev/null | jq . 2>/dev/null || echo "Could not decode token")
    echo "$decoded" | jq '{
        iss: .iss,
        aud: .aud,
        sub: .sub,
        exp: .exp,
        iat: .iat,
        org_id: .org_id,
        account_number: .account_number
    }' 2>/dev/null || echo "$decoded"
fi

echo ""
echo_success "=== JWT Token ==="
echo "$JWT_TOKEN"
echo ""

# Step 5: Show usage example
echo_info "Usage Example:"
echo "  curl -H \"Authorization: Bearer $JWT_TOKEN\" \\"
echo "       https://your-envoy-endpoint/v1/upload"
echo ""

# Save token to file if requested
if [ "${SAVE_TO_FILE:-}" = "true" ]; then
    token_file="${TOKEN_FILE:-jwt-token.txt}"
    echo "$JWT_TOKEN" > "$token_file"
    echo_success "Token saved to: $token_file"
fi

