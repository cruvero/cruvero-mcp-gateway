# GitOps Rollout and Rollback Playbook

This playbook applies to the Argo CD resources in `deploy/argocd/project.yaml` and `deploy/argocd/applicationset.yaml`.

## Environment Mapping

- Dev application: `mcpgateway-dev` in namespace `mcpgateway-dev`
- Staging application: `mcpgateway-staging` in namespace `mcpgateway-staging`
- Prod application: `mcpgateway-prod` in namespace `mcpgateway-prod`

## Dev Rollout Validation Checklist

1. Confirm Argo app health and sync status.
   - `argocd app get mcpgateway-dev`
   - `argocd app wait mcpgateway-dev --health --sync --timeout 300`
2. Confirm pod readiness.
   - `kubectl -n mcpgateway-dev get pods`
   - `kubectl -n mcpgateway-dev rollout status deploy/mcpgateway-dev --timeout=300s`
3. Confirm service reachability.
   - `kubectl -n mcpgateway-dev get svc mcpgateway-dev`
4. Confirm health endpoints respond.
   - `kubectl -n mcpgateway-dev port-forward svc/mcpgateway-dev 8443:8443`
   - `curl -k https://127.0.0.1:8443/healthz`
   - `curl -k https://127.0.0.1:8443/readyz`
5. Confirm metrics endpoint serves Prometheus format.
   - `kubectl -n mcpgateway-dev port-forward svc/mcpgateway-dev 9090:9090`
   - `curl -s http://127.0.0.1:9090/metrics | head`
   - `curl -s http://127.0.0.1:9090/metrics | grep "^# HELP mcpgw_http_requests_total"`

## Staging and Prod Promotion Checklist

Staging and prod are blocked by deny sync windows by default.

1. Confirm dev rollout is healthy and stable.
2. Create a Git change to temporarily open the sync window for the target environment in `deploy/argocd/project.yaml`.
3. Merge and sync the AppProject.
   - `argocd app sync mcpgateway-staging` or `argocd app sync mcpgateway-prod`
4. Run the same validation checklist as dev, replacing app and namespace.
5. Reapply default deny policy by restoring the sync window block in Git and syncing again.

## Rollback Procedure

1. Revert the problematic Git commit(s).
   - `git revert <sha>`
   - `git push`
2. Sync the affected Argo application.
   - `argocd app sync mcpgateway-dev`
   - `argocd app sync mcpgateway-staging`
   - `argocd app sync mcpgateway-prod`
3. Validate recovery:
   - `argocd app wait <app> --health --sync --timeout 300`
   - `kubectl -n <namespace> get pods`
   - `/healthz` and `/readyz` return success
   - `/metrics` remains available and in Prometheus format
