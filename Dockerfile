# syntax=docker/dockerfile:1

# ── Build stage ──────────────────────────────────────────────
ARG DOCKER_PROXY=docker.io
ARG GCR_PROXY=gcr.io

FROM ${DOCKER_PROXY}/library/golang:1.26.1-bookworm@sha256:c7a82e9e2df2fea5d8cb62a16aa6f796d2b2ed81ccad4ddd2bc9f0d22936c3f2 AS build

ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=1 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT} \
      -X main.buildDate=${BUILD_DATE}" \
    -o /out/mcpgw ./cmd/mcpgw

# ── ONNX Runtime stage ──────────────────────────────────────
FROM ${DOCKER_PROXY}/library/debian:bookworm-slim AS onnx-dl

ARG ORT_VERSION=1.24.1
ARG ORT_SHA256=9142552248b735920f9390027e4512a2cacf8946a1ffcbe9071a5c210531026f
RUN apt-get update && apt-get install -y --no-install-recommends wget ca-certificates \
    && ORT_TGZ="onnxruntime-linux-x64-${ORT_VERSION}.tgz" \
    && wget -q "https://github.com/microsoft/onnxruntime/releases/download/v${ORT_VERSION}/${ORT_TGZ}" \
    && echo "${ORT_SHA256}  ${ORT_TGZ}" | sha256sum -c - \
    && tar -xzf "${ORT_TGZ}" \
    && cp onnxruntime-linux-x64-${ORT_VERSION}/lib/libonnxruntime.so* /usr/lib/ \
    && rm -rf "onnxruntime-linux-x64-${ORT_VERSION}" "${ORT_TGZ}"

# ── Model download stage ────────────────────────────────────
FROM ${DOCKER_PROXY}/library/debian:bookworm-slim AS model-dl

ARG MODEL_SHA256=6fd5d72fe4589f189f8ebc006442dbb529bb7ce38f8082112682524616046452
ARG TOKENIZER_SHA256=be50c3628f2bf5bb5e3a7f17b1f74611b2561a3a27eeab05e5aa30f411572037
RUN apt-get update && apt-get install -y --no-install-recommends wget ca-certificates \
    && mkdir -p /models \
    && wget -q -O /models/all-MiniLM-L6-v2.onnx \
       "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx" \
    && echo "${MODEL_SHA256}  /models/all-MiniLM-L6-v2.onnx" | sha256sum -c - \
    && wget -q -O /models/tokenizer.json \
       "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/tokenizer.json" \
    && echo "${TOKENIZER_SHA256}  /models/tokenizer.json" | sha256sum -c -

# ── Runtime stage ────────────────────────────────────────────
FROM ${GCR_PROXY}/distroless/cc-debian12:nonroot

COPY --from=build /out/mcpgw /mcpgw
COPY --from=onnx-dl /usr/lib/libonnxruntime.so* /usr/lib/
COPY --from=model-dl /models/ /models/
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY migrations /migrations

EXPOSE 8443 9090
USER nonroot:nonroot
ENTRYPOINT ["/mcpgw", "serve"]
