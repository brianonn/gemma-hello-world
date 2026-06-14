#!/bin/bash
# entrypoint.sh
set -e

# Function to fetch secrets from HashiCorp Vault and inject into environment
# This follows the requirement for secure secret injection at container startup
fetch_vault_secrets() {
    local path=$1
    if [[ -n "$VAULT_TOKEN" && -n "$VAULT_ADDR" ]]; then
        echo "Fetching secrets from $VAULT_ADDR/v1/$path"
        # Fetch secret and parse with jq, exporting each key-value pair as an env var
        # Note: Requires 'jq' to be installed in the container image
        export $(curl -s -H "X-Vault-Token: $VAULT_TOKEN" "$VAULT_ADDR/v1/$path" | jq -r '.data.data | to_entries | .[] | "\(.key)=\(.value)"')
    else
        echo "Vault credentials not found, skipping secret injection"
    fi
}

# Inject secrets if a path is provided via ENV
if [[ -n "$VAULT_SECRET_PATH" ]]; then
    fetch_vault_secrets "$VAULT_SECRET_PATH"
fi

# Execute the CMD from Dockerfile
exec "$@"
