# syntax=docker/dockerfile:1.7
#
# Multi-stage build for the Go port of gitops-playground.
#
#   Stage 1 (builder): cross-compiles ./cmd/gop using BUILDPLATFORM.
#   Stage 2 (tools)  : downloads helm + kubectl for TARGETARCH with SHA256 check.
#   Stage 3 (final)  : distroless/static-debian12:nonroot. Both helm v4 and
#                       kubectl are CGO-free static binaries and run fine in
#                       distroless without glibc.
#
# Build with: docker buildx build --platform linux/amd64,linux/arm64 -t gop .
# from the go_src directory.

ARG GO_VERSION=1.22
ARG ALPINE_VERSION=3.20
ARG HELM_VERSION=4.1.4
ARG KUBECTL_VERSION=1.35.4

# SHA256s pinned per architecture. Update when bumping the versions above.
# Last verified 2026-06-12 against upstream:
#   helm   : https://get.helm.sh/helm-v${HELM_VERSION}-linux-${arch}.tar.gz.sha256sum
#   kubectl: https://dl.k8s.io/release/v${KUBECTL_VERSION}/bin/linux/${arch}/kubectl.sha256
ARG HELM_SHA256_AMD64=70b2c30a19da4db264dfd68c8a3664e05093a361cefd89572ffb36f8abfa3d09
ARG HELM_SHA256_ARM64=13d03672be289045d2ff00e4e345d61de1c6f21c1257a45955a30e8ae036d8f1
ARG KUBECTL_SHA256_AMD64=b529430df69a688fd61b64ad2299edb5fd71cb58be2a4779dba624c7d3510efd
ARG KUBECTL_SHA256_ARM64=6a5a4cc4e396d7626a7a693a3044b51c75520f81db30fe6816c2554e53be336f

# ---------------------------------------------------------------------------
# Stage 1: Go build (native on BUILDPLATFORM, cross-compiles to TARGETARCH)
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown

ENV CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOFLAGS=-mod=readonly

WORKDIR /src

# Modules first so the layer caches across source edits.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build \
      -trimpath \
      -ldflags="-s -w \
        -X github.com/cloudogu/gitops-playground/go/internal/cli.Version=${VERSION} \
        -X github.com/cloudogu/gitops-playground/go/internal/cli.Commit=${COMMIT}" \
      -o /out/gop \
      ./cmd/gop

# ---------------------------------------------------------------------------
# Stage 2: Tools downloader (helm + kubectl, SHA256-verified)
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM alpine:${ALPINE_VERSION} AS tools

ARG TARGETARCH
ARG HELM_VERSION
ARG KUBECTL_VERSION
ARG HELM_SHA256_AMD64
ARG HELM_SHA256_ARM64
ARG KUBECTL_SHA256_AMD64
ARG KUBECTL_SHA256_ARM64

RUN apk add --no-cache curl tar ca-certificates

WORKDIR /tools

RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) HELM_SHA="${HELM_SHA256_AMD64}"; KUBECTL_SHA="${KUBECTL_SHA256_AMD64}" ;; \
      arm64) HELM_SHA="${HELM_SHA256_ARM64}"; KUBECTL_SHA="${KUBECTL_SHA256_ARM64}" ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    # helm
    curl -fsSL --retry 5 --retry-delay 2 \
        -o helm.tar.gz \
        "https://get.helm.sh/helm-v${HELM_VERSION}-linux-${TARGETARCH}.tar.gz"; \
    echo "${HELM_SHA}  helm.tar.gz" | sha256sum -c -; \
    tar -xzf helm.tar.gz; \
    mv "linux-${TARGETARCH}/helm" /tools/helm; \
    chmod 0755 /tools/helm; \
    rm -rf helm.tar.gz "linux-${TARGETARCH}"; \
    # kubectl
    curl -fsSL --retry 5 --retry-delay 2 \
        -o kubectl \
        "https://dl.k8s.io/release/v${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl"; \
    echo "${KUBECTL_SHA}  kubectl" | sha256sum -c -; \
    chmod 0755 kubectl

# ---------------------------------------------------------------------------
# Stage 3: Runtime — distroless static, non-root
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.source="https://github.com/cloudogu/gitops-playground" \
      org.opencontainers.image.title="gop" \
      org.opencontainers.image.description="GitOps Playground CLI (Go port)" \
      org.opencontainers.image.licenses="AGPL-3.0-only"

COPY --from=tools    --chown=nonroot:nonroot /tools/helm    /usr/local/bin/helm
COPY --from=tools    --chown=nonroot:nonroot /tools/kubectl /usr/local/bin/kubectl
COPY --from=builder  --chown=nonroot:nonroot /out/gop       /usr/local/bin/gop

USER nonroot:nonroot
WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/gop"]
