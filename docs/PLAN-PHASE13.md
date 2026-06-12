# Phase 13 — Plan

This document captures what Phase 13 sets out to do, why each work
package is shaped the way it is, the order in which the work packages
have to land, and which can run in parallel sub-agents.

## 0. Inputs

- Phases 0–12 are merged into `feature/go_port`.
- Iteration-3 (TEST_PLAN.md) proved that the minimal profile boots
  end-to-end against a real cluster, surfaced five real bugs (F-8..F-12)
  and five further task tickets (T-5..T-10).
- The CI pipeline (`.github/workflows/ci.yml`) runs `go vet`, `golangci-lint`,
  `go test -race`, `go build`, `docker buildx` (amd64+arm64) and
  `nix flake check`. All six were rehearsed locally in Phase 13a and are
  green. The only fix required there was the `flake-utils.lib.mkApp`
  meta argument (commit `efe25207`).

## 1. Goals

- Make `gop --profile=full` reach a sensible terminal state on a fresh
  cluster.
- Tighten the four Raw-map / Stub seams that show up most often in
  current code review.
- Keep the tree small and idiomatic. Net code growth this phase should
  be ≤ +2.5k lines (today: 17.0k).

Non-goals:

- Build-time generators for the Groovy schema (the reflective dump in
  `internal/config/schema_keys_test.go` is enough).
- Operator-profile parity (separate track).

## 2. Architecture findings worth acting on

These came out of the Phase 12 audit (logged in REMAINING.md) and the
Phase 13a code sweep.

### F-Arch-1. Raw maps drive seven files of boilerplate

`internal/config/schema.go` holds `Scm` and `MultiTenant` as
`map[string]any` wrappers. Every consumer (wire.go × 3 sites,
configurator.go × 3 sites, features/scmmanager/scmmanager.go × 5 sites,
features/argocd/setup.go × 1 site) repeats the same
`(map[string]any).(string)` ladder. Typed structs collapse that into a
single read.

Acceptance: zero call sites refer to `cfg.Scm.Raw` or
`cfg.MultiTenant.Raw` after T-5 lands. `scmmString` and friends in
`internal/features/scmmanager/scmmanager.go` go away.

### F-Arch-2. argocd template tree is read from disk

`internal/features/argocd/setup.go` reads `argocd/cluster-resources/…`
relative to the working directory. After phase 8 that path lives only
under `retired/argocd/…`. Today it works only when a symlink is in
place. Embed via `go:embed` (mirrors `internal/profile/profiles`).

Acceptance: `gop` produced by `nix build` works against a temp dir
without any on-disk asset.

### F-Arch-3. ArgoCD `Install` stops at "repo pushed"

`internal/features/argocd/argocd.go:Install` is documented as stopping
before the helm umbrella install and the `argocd-secret` bcrypt patch.
Without the rest, `--profile=full` cannot succeed.

Acceptance: after `gop --profile=full --yes` against a fresh cluster,
the `argocd` namespace shows `argo-cd-server`, `argo-cd-application-
controller` and `argo-cd-repo-server` pods Running, plus
`argocd-secret` carries the bcrypt of the admin password.

### F-Arch-4. SCM-Manager bootstrap config lives only in the
internal install path

`internal/features/scmmanager/setup.go:Configure` runs only when SCMM
is internal. Out-of-cluster operation (in the test harness today, but
also legitimate "SCMM-as-a-service" deployments) silently skips
`namespaceStrategy: CustomNamespaceStrategy`, the plugin install, and
the Jenkins-plugin config — every subsequent operation against SCMM
breaks.

Acceptance: `gop` calls `Configure` regardless of internal vs external,
once the SCMM API is reachable.

### F-Arch-5. ContentLoader is two stubs and one half-implementation

`internal/content/copy.go` and `folder_based.go` are stubbed; only
`mirror.go` is real. `helm_release.go` carries the data model but no
strategy call. `--profile=content-examples` and the helm-release
profiles cannot work yet.

Acceptance: COPY + FOLDER_BASED + HelmRelease each produce the same
SCMM-repo content the Groovy original did, verified by a focused
test against the embedded scm-manager fake from `internal/scm`.

### F-Arch-6. Runner has a `PersistConfig` hook but no implementation

Application start should write the resolved Config into a
`gop-configuration` Secret in `cfg.Application.GopNamespace`. That
hook in `internal/runner/runner.go` is left nil. Trivial fix.

### F-Arch-7. OpenShift UID lookup is an injection seam without a
filler

`internal/features/monitoring.Feature.OpenShiftUID` is wired through
but nobody fills it. On OpenShift, monitoring fails silently. Small
helper in `internal/k8s` reads the
`openshift.io/sa.scc.uid-range` annotation.

## 3. Work packages

Reading legend per WP:

- **inputs**: files / tickets that must already exist
- **outputs**: files touched + tests required
- **conflicts**: which other WPs can NOT land in the same wave
- **size**: rough LOC estimate

| Id | Title | size | conflicts |
| --- | --- | --- | --- |
| WP-A1 | T-7 embed argocd cluster-resources tree | ~120 | — |
| WP-A2 | P1.5 OpenShift UID helper (k8s) | ~80 | — |
| WP-A3 | P1.6 PersistConfig in runner | ~70 | — |
| WP-A4 | T-1 k8s benchmark file | ~60 | — |
| WP-B1 | T-5 typed scm.* / multiTenant.* | ~350 | wire.go, scmmanager feature |
| WP-B2 | T-8 SCMM Configure for external setups | ~120 | scmmanager feature (after WP-B1) |
| WP-B3 | T-9 ArgoCD Install full (helm + secret patch) | ~250 | argocd feature |
| WP-C1 | P1.4a Content COPY | ~200 | content package |
| WP-C2 | P1.4b Content FOLDER_BASED | ~200 | content package |
| WP-C3 | P1.8 ContentSchema HelmReleases code path | ~120 | — |
| WP-C4 | P2.10 Runner e2e test with fake k8s | ~250 | — |

### Wave A — independent, all sub-agents in parallel

WP-A1, WP-A2, WP-A3, WP-A4. None touch the same files. Each gets its
own sub-agent. Time-budget per sub-agent: short report, < 250 words.

### Wave B — typed schema first, then two big sub-agents

WP-B1 lands first because both WP-B2 and WP-B3 read from the typed
schema. After WP-B1 the two larger sub-agents (B2, B3) run in
parallel — they touch disjoint packages.

### Wave C — content loader + e2e runner test, all parallel

WP-C1, WP-C2, WP-C3, WP-C4. The three content WPs touch
`internal/content/`, but on disjoint files (`copy.go`,
`folder_based.go`, `helm_release.go`). WP-C4 lives in
`internal/runner/runner_e2e_test.go` with build tag `e2e` so the race
detector run keeps the existing scope.

## 4. Conflict map

```
WP-A1 ───────────┐
WP-A2 ───────────┤   (no overlap)
WP-A3 ───────────┤
WP-A4 ───────────┘

WP-B1 ── schema.go / configurator.go / wire.go / scmmanager feature
   │
   ├─→ WP-B2 ── scmmanager feature (additive)
   └─→ WP-B3 ── argocd feature (no overlap with B1/B2)

WP-C1 ── content/copy.go
WP-C2 ── content/folder_based.go
WP-C3 ── content/helm_release.go
WP-C4 ── runner/runner_e2e_test.go (new file)
```

## 5. Acceptance for the whole phase

Pipeline (the six steps Phase 13a rehearsed) stays green after every
wave:

1. `go mod tidy` and `git diff --exit-code go.mod go.sum`
2. `go vet ./...`
3. `go test -race -count=1 ./...`
4. `golangci-lint run ./...`
5. `go build -trimpath -o bin/gop ./cmd/gop`
6. `nix flake check`
7. `podman build --platform linux/amd64,linux/arm64 -t gop:dev .`

`gop --profile=full --yes` against a fresh minikube ends with:

- `argocd`, `monitoring`, `secrets`, `cert-manager`, `ingress`,
  `scm-manager`, `jenkins`, `registry` namespaces present.
- All matching helm releases `deployed` and pod count > 0.
- SCMM has the three expected repos (`argocd/cluster-resources`,
  `argocd/example-apps`, `3rd-party-dependencies/*`).
- Argo CD admin password matches `cfg.Application.Password`.

If the runner cannot finish (cluster too small, image pulls flaky,
external dependency outage), TEST_PLAN.md gets an entry that captures
exactly which step the run reached and why it stopped. We will not
claim a clean green if it is not green.

## 6. Risks worth calling out

- **R-1**: `go:embed` cannot embed files outside the module. The
  argocd templates currently live under `retired/argocd/`. WP-A1
  moves a curated copy into `internal/features/argocd/embed/` —
  retired/ stays untouched. ~6 MB tree.
- **R-2**: T-5 has to keep YAML backward compatibility. The strict-
  decode test (`internal/config/profile_compat_test.go`) is the
  guard rail; the WP-B1 brief mandates running it green.
- **R-3**: Sub-agents cannot run `go build`. We require each
  brief to dump every uncertainty into the final report; the
  driver runs `vet/test/lint` per package after each return.
- **R-4**: Content COPY needs `git.Service` semantics we haven't
  exercised (subtree copy with .git skipping). WP-C1 brief
  explicitly lists what the agent may NOT change in `internal/git`.

## 7. Operating notes

- Commit cadence: one commit per WP, conventional commit, REMAINING.md
  + TEST_PLAN.md edits in the same commit. Co-author trailer mandatory.
- Push after every commit (the user asked for this explicitly).
- Sub-agents are dispatched in waves; the driver waits for each wave
  to return before starting the next.
- If a sub-agent returns a partial result, the driver finishes the
  work in the foreground rather than dispatching a second sub-agent
  for the same WP.

## 8. Out of scope for Phase 13

- Helm/kubectl version bumps.
- Migration of profile YAMLs out of `internal/profile/profiles/`.
- Public packages under `pkg/`.
- Anything in `retired/`.
