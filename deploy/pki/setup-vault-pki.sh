#!/usr/bin/env bash
set -euo pipefail

PKI_PATH="${PKI_PATH:-pki_mcp}"
TRUST_DOMAIN="${TRUST_DOMAIN:-example.com}"
VAULT_K8S_ROLE="${VAULT_K8S_ROLE:-cert-manager-pki}"
VAULT_POLICY_NAME="${VAULT_POLICY_NAME:-cert-manager-mcp-pki}"
CERT_MANAGER_SA="${CERT_MANAGER_SA:-cert-manager}"
CERT_MANAGER_NS="${CERT_MANAGER_NS:-cert-manager}"
MAX_TTL="${MAX_TTL:-87600h}"
ISSUE_TTL="${ISSUE_TTL:-720h}"
SPIFFE_PREFIX="spiffe://${TRUST_DOMAIN}/ns/${APP_NAMESPACE:-myapp-dev}/sa/"

if [[ -z "${VAULT_ADDR:-}" || -z "${VAULT_TOKEN:-}" ]]; then
  echo "VAULT_ADDR and VAULT_TOKEN must be set" >&2
  exit 1
fi

if ! vault secrets list -format=json | jq -e --arg path "${PKI_PATH}/" '.[$path]' >/dev/null; then
  vault secrets enable -path="${PKI_PATH}" pki
fi

vault secrets tune -max-lease-ttl="${MAX_TTL}" "${PKI_PATH}"

if ! vault read -field=certificate "${PKI_PATH}/cert/ca" >/dev/null 2>&1; then
  vault write -field=certificate "${PKI_PATH}/root/generate/internal" \
    common_name="${TRUST_DOMAIN} MCP Root CA" \
    ttl="${MAX_TTL}" >/tmp/${PKI_PATH}_ca.crt
fi

vault write "${PKI_PATH}/config/urls" \
  issuing_certificates="${VAULT_ADDR}/v1/${PKI_PATH}/ca" \
  crl_distribution_points="${VAULT_ADDR}/v1/${PKI_PATH}/crl"

cat >/tmp/${VAULT_POLICY_NAME}.hcl <<POLICY
path "${PKI_PATH}/sign/mcpgateway-dev-server" {
  capabilities = ["update"]
}
path "${PKI_PATH}/sign/mcp-backend-client" {
  capabilities = ["update"]
}
path "${PKI_PATH}/issue/mcpgateway-dev-server" {
  capabilities = ["update"]
}
path "${PKI_PATH}/issue/mcp-backend-client" {
  capabilities = ["update"]
}
POLICY

vault policy write "${VAULT_POLICY_NAME}" /tmp/${VAULT_POLICY_NAME}.hcl

vault write "auth/kubernetes/role/${VAULT_K8S_ROLE}" \
  bound_service_account_names="${CERT_MANAGER_SA}" \
  bound_service_account_namespaces="${CERT_MANAGER_NS}" \
  policies="${VAULT_POLICY_NAME}" \
  ttl="1h"

vault write "${PKI_PATH}/roles/mcpgateway-dev-server" \
  ttl="${ISSUE_TTL}" \
  max_ttl="${ISSUE_TTL}" \
  allow_any_name=false \
  allow_bare_domains=true \
  allow_subdomains=true \
  enforce_hostnames=true \
  allow_localhost=false \
  allowed_domains="mcpgateway-dev,mcpgateway-dev.${APP_NAMESPACE:-myapp-dev},mcpgateway-dev.${APP_NAMESPACE:-myapp-dev}.svc,mcpgateway-dev.${APP_NAMESPACE:-myapp-dev}.svc.cluster.local" \
  require_cn=false \
  server_flag=true \
  client_flag=false \
  key_type="rsa" \
  key_bits=2048

vault write "${PKI_PATH}/roles/mcp-backend-client" \
  ttl="${ISSUE_TTL}" \
  max_ttl="${ISSUE_TTL}" \
  allow_any_name=true \
  enforce_hostnames=false \
  allowed_uri_sans="${SPIFFE_PREFIX}*" \
  require_cn=false \
  server_flag=false \
  client_flag=true \
  key_type="rsa" \
  key_bits=2048

echo "Vault PKI bootstrap complete for ${PKI_PATH}" 
