# GitOps Playground (Go rewrite)

CLI that bootstraps a GitOps stack — Argo CD, SCM-Manager, Jenkins, Vault,
Monitoring, Ingress, CertManager, External Secrets, Registry — into a
Kubernetes cluster from a single command.

This repository is the **Go rewrite** of the original Groovy/Micronaut
`gitops-playground` tool. The legacy sources live under
[`retired/`](retired/) and remain buildable for reference.

## Quick start

### With Nix (recommended for hacking)

```sh
nix develop      # opens a shell with go, helm, kubectl, golangci-lint, …
go mod tidy      # one-shot to materialise go.sum
nix build        # cross-compiles ./cmd/gop and runs tests
./result/bin/gop --help
```

### Without Nix

Install Go ≥ 1.22, then:

```sh
go mod tidy
make build       # produces ./bin/gop
make test
./bin/gop --help
```

### Container image

```sh
docker buildx build --platform linux/amd64,linux/arm64 -t gop:dev .
docker run --rm -v ~/.kube:/workspace/.kube gop:dev --help
```

The image is a [`distroless/static-debian12:nonroot`](https://github.com/GoogleContainerTools/distroless) layer with
`gop`, `helm` and `kubectl` baked in (SHA256-verified during build).
CI publishes to `ghcr.io/<owner>/gitops-playground` on every `main`
commit.

## What it does

`gop` reads a layered configuration (defaults → embedded profile →
`--config-map` → `--config-file` → CLI flags) and turns each enabled
*feature* into a Helm release plus the supporting Kubernetes objects
(namespaces, secrets, RBAC, ConfigMaps). Each feature is independent;
the runner orders them deterministically and stops on the first error.
A `--destroy` pass tears the same set down.

See [`PORTING_PLAN.md`](PORTING_PLAN.md) for the architecture and
[`SPECS.md`](SPECS.md) for the per-adapter contracts.

## Repository layout

```
.
├── cmd/gop/          entry point
├── internal/
│   ├── cli/          Cobra command tree, flag wiring, config pipeline
│   ├── config/       typed schema + YAML loader + ApplicationConfigurator
│   ├── deployment/   Helm / Argo CD strategies
│   ├── destroy/      teardown handlers (argocd / jenkins / scmm)
│   ├── exec/         os/exec wrapper (pipe-safe)
│   ├── feature/      Feature interface + registry + image-pull helper
│   ├── features/     concrete features (argocd, jenkins, …)
│   ├── fsutil/       small file helpers
│   ├── git/          go-git wrapper
│   ├── helm/         helm CLI wrapper
│   ├── httpx/        opt-in insecure TLS + retry transport
│   ├── jenkins/      Jenkins REST adapter
│   ├── k8s/          client-go wrapper (split per resource)
│   ├── log/          slog setup (--debug / --trace)
│   ├── profile/      embedded application-*.yaml profiles
│   ├── runner/       Application.start() port
│   ├── scm/          SCM provider abstraction + scmmanager / gitlab
│   ├── template/     text/template helpers
│   ├── wire/         dependency wiring graph
│   └── content/      ContentLoader (MIRROR done; COPY/FOLDER_BASED stubs)
├── retired/          legacy Groovy/Maven tree (build target: archived)
├── Dockerfile        multi-stage distroless image
├── Makefile          build / test / lint
├── flake.nix         devShell + buildGoModule + dockerTools image
├── AGENTS.md         contract for AI agents working in this repo
├── BUILD.md          first-time build notes
├── REMAINING.md      prioritised open work
├── PORTING_PLAN.md   high-level rewrite plan
└── SPECS.md          per-adapter design specs
```

## Status

- Phases 0 – 7 of the rewrite are in. The runner installs, the
  destroyer tears down, the container builds, the Nix flake reproduces
  the binary, CI runs tests + image build.
- Known gaps and the order they are tackled in live in
  [`REMAINING.md`](REMAINING.md).

## License

AGPL-3.0-only (inherited from the original project). See
[`LICENSE`](LICENSE).
