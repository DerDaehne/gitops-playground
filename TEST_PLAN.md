# Test plan and findings

Living document. Every iteration that runs the toolchain against this
repo records what it ran, what it found and what it changed. New
entries go to the bottom.

## Scope

Three layers of verification, run in this order:

1. **Static checks**
   - `go vet ./...`
   - `go build ./...`
   - `go build -o bin/gop ./cmd/gop`
2. **Unit tests**
   - `go test -race -count=1 ./...`
3. **Smoke tests** against the built binary
   - `bin/gop --version`
   - `bin/gop --help`
   - `bin/gop --profile=<name> --output-config-file` for every profile
     bundled under `internal/profile/profiles/`
4. **Reproducibility**
   - `nix flake check` (when nix is available)
   - `docker buildx build .` (deferred until phase 10; not part of every
     CI run because it pulls helm + kubectl)

End-to-end tests against a real cluster are out of scope for this plan
— see `REMAINING.md` P2.10.

## Iteration log

### 2026-06-11 — iteration 1 (claude-4.7, branch `feature/go_port`)

#### Environment

- Go 1.26.3 (from `nixpkgs#go` via `nix shell`)
- Nix 2.34.7
- No live Kubernetes cluster
- This was the first time the toolchain ran against the rewritten tree.

#### Setup

- `nix flake lock` generated `flake.lock` (nixpkgs `a799d3e3`,
  flake-utils `11707dc2`).
- `go mod tidy` materialised `go.sum` (262 lines, all expected
  transitives from client-go, go-git, x/crypto landed).

#### Findings

| # | Finding | Location | Severity | Resolution |
| - | --- | --- | --- | --- |
| F-1 | `agentValues(cfg, dockerGid)` called with two args in test; the function only accepts one (`dockerGid`). | `internal/features/jenkins/jenkins_test.go:261,269` | blocks `go vet` and `go test` in the package | replaced both calls with the one-arg form; dropped the unused `cfg := config.New()` setup. |
| F-2 | "external missing username" / "external missing password" test cases did not zero the corresponding field before mutating, so `config.New()` defaults (`admin`, generated password) silently satisfied the check. | `internal/features/jenkins/jenkins_test.go:80-96` | test passed for the wrong reason | each case now explicitly sets the field to `""` after `config.New()`. |

No other build / vet / test failures across 22 packages.

#### Results after the two fixes

- `go vet ./...` — clean
- `go build ./...` — clean
- `go test -race -count=1 ./...` — every package green, ~16 s total
  (k8s is the slowest at 1.5 s)
- `bin/gop --version` — `gitops-playground (GOP) dev`
- `bin/gop --help` — Cobra renders all flags
- `bin/gop --profile=minimal --output-config-file` — emits a sensible
  baseline YAML

Surprises (in a good way):

- The wire graph compiled at first try after the spec-driven fixes
  applied in phase 4d. The four cross-package signature corrections
  pre-applied during phase 5 / 6 (PatchJSONMerge, namespace/name in
  ApplyDockerConfigSecret, scmmanager.Config field shape,
  EnsureNamespace shape) covered every mismatch.
- Sub-agent tests for k8s/jenkins/scm/git all green on first race-tested
  run.

#### Doings spawned by this iteration

- `REMAINING.md` P0.3 ("expected mechanical compile errors") is
  **closed**: only the two findings above materialised. The remaining
  P0 items are unchanged (`go.sum` commit, `vendorHash` pin).
- New P2 task (added to REMAINING.md, see entry "T-1"): run
  `go test -bench=.` on `internal/k8s` once benchmarks exist — the
  package already sits at 1.5 s and has the deepest poll loops, so it
  is the natural place for regression detection on timing.

#### Caveats

- We have not yet run `nix build` end-to-end. The flake's `vendorHash`
  is still `pkgs.lib.fakeHash`; first `nix build` will print the real
  value to paste into the flake. Tracked in REMAINING.md P0.2.
- The Docker image was not built in this iteration; the SHA256s for
  helm/kubectl in `Dockerfile` are taken from the original Maven
  Dockerfile (kubectl) and from a sub-agent web research (helm). They
  must be verified before the first `docker buildx build` lands.
  Tracked as **T-2** (added below).

#### Tasks added to REMAINING.md

- **T-1 (P2.new)**: `internal/k8s` benchmark coverage.
- **T-2 (P0.new)**: verify helm/kubectl SHA256s in `Dockerfile` against
  upstream release pages before the first container build.

---

## What "passing" means

A change is allowed to merge when:

- `nix develop -c bash -lc "go vet ./... && go test -race -count=1 ./... && go build -trimpath -o bin/gop ./cmd/gop"` exits 0.
- Smoke tests above produce valid output.
- The change updated `REMAINING.md` and (if architecture moved)
  `SPECS.md` / `PORTING_PLAN.md`.
- A new iteration log entry exists at the bottom of this file with
  what was run, what was found, what was fixed.

When you cannot run the toolchain locally (sandboxed agent, no Go
available), record that loudly in the iteration log instead of skipping
the section. Honest "I could not run this" beats silent assumption.
