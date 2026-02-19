# syntax=docker/dockerfile:1
ARG DOCKER_PROXY=docker-cache.dev.gchinfo.com
ARG GCR_PROXY=gcr-cache.dev.gchinfo.com
FROM ${DOCKER_PROXY}/library/golang:1.25.7-alpine AS build
WORKDIR /src

ARG GOPROXY=https://nexus.dev.gchinfo.com/repository/go-proxy/,direct
ENV GOPROXY=$GOPROXY

COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/mcpgw ./cmd/mcpgw

FROM ${GCR_PROXY}/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/mcpgw /mcpgw
COPY migrations /migrations
EXPOSE 8443 9090
USER nonroot:nonroot
ENTRYPOINT ["/mcpgw", "serve"]
