FROM mcr.microsoft.com/devcontainers/go:1-1.25-bookworm

ARG HELM_VERSION=v3.17.0
ARG KUBECTL_VERSION=v1.31.0
ARG ARGOCD_VERSION=v2.13.3
ARG GOLANGCI_LINT_VERSION=v1.64.8

USER root

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl git make tar gzip postgresql-client; \
    rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
      amd64) bin_arch="amd64" ;; \
      arm64) bin_arch="arm64" ;; \
      *) echo "unsupported architecture: ${arch}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-${bin_arch}.tar.gz" -o /tmp/helm.tar.gz; \
    tar -xzf /tmp/helm.tar.gz -C /tmp; \
    install -m 0755 "/tmp/linux-${bin_arch}/helm" /usr/local/bin/helm; \
    rm -rf /tmp/helm.tar.gz "/tmp/linux-${bin_arch}"

RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
      amd64) bin_arch="amd64" ;; \
      arm64) bin_arch="arm64" ;; \
      *) echo "unsupported architecture: ${arch}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${bin_arch}/kubectl" -o /usr/local/bin/kubectl; \
    chmod +x /usr/local/bin/kubectl

RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
      amd64) bin_arch="amd64" ;; \
      arm64) bin_arch="arm64" ;; \
      *) echo "unsupported architecture: ${arch}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://github.com/argoproj/argo-cd/releases/download/${ARGOCD_VERSION}/argocd-linux-${bin_arch}" -o /usr/local/bin/argocd; \
    chmod +x /usr/local/bin/argocd

RUN set -eux; \
    curl -fsSL "https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh" | sh -s -- -b /usr/local/bin "${GOLANGCI_LINT_VERSION}"

RUN set -eux; \
    go install honnef.co/go/tools/cmd/staticcheck@latest; \
    go install golang.org/x/vuln/cmd/govulncheck@latest; \
    go install github.com/securego/gosec/v2/cmd/gosec@latest; \
    go install github.com/mibk/dupl@latest

USER vscode
