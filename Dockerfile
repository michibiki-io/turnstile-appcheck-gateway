ARG GO_VERSION=1.26.2
ARG BUILD_COMMIT=unknown
ARG BUILD_VERSION=dev

FROM node:24-bookworm-slim AS frontend
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend ./
RUN npm run check && npm run build

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    /usr/local/go/bin/go mod download

COPY . .
COPY --from=frontend /src/internal/adminui/dist ./internal/adminui/dist
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG BUILD_COMMIT
ARG BUILD_VERSION
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    /usr/local/go/bin/go test ./...
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    export CGO_ENABLED=0; \
    export GOOS="${TARGETOS:-linux}"; \
    export GOARCH="${TARGETARCH:-amd64}"; \
    case "${TARGETARCH:-amd64}/${TARGETVARIANT:-}" in \
      arm/v6) export GOARM=6 ;; \
      arm/v7) export GOARM=7 ;; \
    esac; \
    /usr/local/go/bin/go build \
      -trimpath \
      -buildvcs=false \
      -ldflags="-s -w -X github.com/michibiki-io/turnstile-appcheck-gateway/internal/version.value=${BUILD_VERSION:-dev} -X github.com/michibiki-io/turnstile-appcheck-gateway/internal/version.commit=${BUILD_COMMIT:-unknown}" \
      -o /out/turnstile-appcheck-gateway \
      ./cmd/turnstile-appcheck-gateway
RUN mkdir -p /out/var-lib-turnstile-appcheck-gateway

FROM scratch
WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/turnstile-appcheck-gateway /app/turnstile-appcheck-gateway
COPY --from=builder --chown=65532:65532 /out/var-lib-turnstile-appcheck-gateway /var/lib/turnstile-appcheck-gateway

EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/app/turnstile-appcheck-gateway"]
