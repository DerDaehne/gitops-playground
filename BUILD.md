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

As of `TEST_PLAN.md` iteration 2 the first build is **clean** on a host
with Go 1.22+ and an unrestricted network. `nix build .#default`,
`make check`, `golangci-lint run` and `bin/gop --help/--version` all
produce a zero-exit-code green run.

What is still worth knowing if something does go wrong:

- `go mod tidy` was already run; `go.sum` is committed. If it complains,
  somebody bumped `go.mod` without running `go mod tidy` (CI catches
  this via `git diff --exit-code go.mod go.sum`).
- `flake.nix`'s `vendorHash` is pinned. After a `go.mod`/`go.sum` bump,
  set it back to `pkgs.lib.fakeHash`, run `nix build`, paste the new
  SHA into the file.
- `golangci-lint` runs **outside** the Nix sandbox because the sandbox
  has no network access for module fetches; the CI workflow runs it as
  its own step.

## Where the Groovy original lives

Source-of-truth for behaviour reference: `retired/src/main/groovy/com/cloudogu/gitops/`.
Every Go package documents the Groovy file(s) it ports. To rebuild the
original side, `cd retired && ./mvnw package`.

## Status of the port

See `PORTING_PLAN.md` and `SPECS.md` for the architecture; the commit
log on `feature/go_port` walks through the phases (0 – 11) in order.
The Application Runner, all nine tool features, the SCM provider
abstraction, and the destroy path are in place. Documented gaps live
in `REMAINING.md`; every toolchain run is logged in `TEST_PLAN.md`.

Current focus areas (from `REMAINING.md`):

- T-2 (P0): verify `helm` and `kubectl` SHA256s in the Dockerfile.
- T-4 (P2): tighten `gitlab.Client.RepoURL` so it does not call
  `context.Background()`.
- P1.4 / P1.8: implement ContentLoader COPY and FOLDER_BASED modes
  plus the HelmReleases code path.
- P1.5: OpenShift-UID discovery for `features/monitoring`.
- P1.6: `runner.Runner.PersistConfig`.
