# Vault PKI bootstrap for MCP Gateway and backends

This directory contains the bootstrap artifacts for Vault PKI + cert-manager integration.

## 1) Configure Vault PKI and Kubernetes auth role

```bash
set -a
source ~/.config/.env
set +a

./deploy/pki/setup-vault-pki.sh
```

## 2) Apply cert-manager ClusterIssuers

```bash
kubectl apply -f deploy/pki/cert-manager-clusterissuers.yaml
kubectl get clusterissuer vault-mcp-gateway-server vault-mcp-backend-client
```

## SPIFFE format

Backend client certificates are constrained to URI SANs with prefix:

`spiffe://<TRUST_DOMAIN>/ns/<APP_NAMESPACE>/sa/`

Gateway validates identities with this prefix using `MCPGW_SPIFFE_ALLOW_PREFIX`.
