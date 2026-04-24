# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.26.2 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    /usr/local/go/bin/go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
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
      -ldflags='-s -w' \
      -o /out/turnstile-appcheck-gateway \
      ./cmd/turnstile-appcheck-gateway

FROM scratch
WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/turnstile-appcheck-gateway /app/turnstile-appcheck-gateway

EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/app/turnstile-appcheck-gateway"]
