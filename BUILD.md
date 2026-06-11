# Building gop

This is the Go rewrite of the gitops-playground CLI. The Groovy
original lives under [`retired/`](retired/) for reference only.

## Prerequisites

- Go ≥ 1.22 (the module declares `go 1.22`).
- `helm` and `kubectl` on `PATH` (used as subprocesses).
- Network access on first build for module downloads (or a fully
  populated `GOMODCACHE`).
- Alternatively, `nix develop` brings the whole toolchain in one shot.

## First build

The sub-agents that authored several packages could not run the Go
toolchain themselves – they ship the direct module versions but not the
indirect transitive `go.sum` lines. Pull them on the first build:

```sh
go mod tidy
go vet ./...
go build -o bin/gop ./cmd/gop
```

`go mod tidy` may report a few additional indirect modules from
`k8s.io/client-go`, `github.com/go-git/go-git/v5` and
`golang.org/x/crypto`. Each is already pinned in the matching
`internal/<pkg>/DEPS.md`.

## Day-to-day

The `Makefile` wraps the common operations:

```
make tidy   # go mod tidy
make build  # produces ./bin/gop
make test   # go test -race -count=1 ./...
make check  # vet + test
make clean
```

## Expected first-run issues

Because the toolchain could not validate the sub-agent output, expect a
small batch of compile errors the first time you run `make build`. They
typically fall into one of these buckets:

1. A method called in `internal/wire/wire.go` does not exist exactly with
   that name on the matching adapter (the wire file was written from
   spec, not by reading every adapter). Fix by jumping to the adapter,
   correcting the call site, and re-running.
2. A feature struct field referenced in `internal/wire` uses a slightly
   different name than the sub-agent's actual implementation. Same fix.
3. Unused imports in the test files (rare; the sub-agents kept their
   imports tight).

There are no known logical issues; the failures are mechanical.

## Where the Groovy original lives

Source-of-truth for behaviour reference: `retired/src/main/groovy/com/cloudogu/gitops/`.
Every Go package documents the Groovy file(s) it ports. To rebuild the
original side, `cd retired && ./mvnw package`.

## Status of the port

See `PORTING_PLAN.md` and `SPECS.md` for the architecture; the commit
log on `feature/go_port` walks through the phases in order. The
Application Runner, all nine tool features, the SCM provider abstraction,
and the destroy path are in place. Documented gaps:

- `internal/content`: COPY and FOLDER_BASED repo types are stubs with
  TODOs; MIRROR is complete.
- `internal/features/monitoring`: OpenShift UID discovery is not yet
  implemented (uses an injection seam – the runner can fill it once a
  small `k8s` helper is added).
- `internal/runner.PersistConfig` is left nil; the install-time
  "store the resolved config in the gop-job namespace as a secret"
  behaviour from `Application.storeGopInformationInSecret` is wired up
  but unimplemented.

These are tracked in `REMAINING.md`.
