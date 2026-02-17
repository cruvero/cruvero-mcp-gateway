# syntax=docker/dockerfile:1
FROM golang:1.25.7-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/mcpgw ./cmd/mcpgw

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/mcpgw /mcpgw
EXPOSE 8443 9090
USER nonroot:nonroot
ENTRYPOINT ["/mcpgw", "serve"]
