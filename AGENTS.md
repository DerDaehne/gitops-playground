# AGENTS.md — guidance for AI agents working in this repo

This file is the contract every AI agent (Claude, Codex, Cursor, Continue,
GitHub Copilot Workspace, …) follows when it edits this repository.
**Read it in full before changing any code.** If something is unclear,
prefer asking the human to clarify over guessing.

> **Scope of this file**: applies to every file in this repository
> except those under `retired/`. The `retired/` tree is the archived
> Groovy original; agents do not touch it unless explicitly asked.

---

## 1. Mission

`gop` is a CLI that bootstraps a complete GitOps stack into a Kubernetes
cluster:

- **Argo CD** for delivery (operator and non-operator modes)
- **SCM-Manager** or **GitLab** as the source of truth
- **Jenkins** for CI
- **Helm**-deployed tools: Monitoring (kube-prometheus-stack), Vault +
  External Secrets, Ingress (Traefik), Cert-Manager, Registry
- **ContentLoader** that pushes user repos / manifests into the chosen
  SCM (mirror, copy, folder-based, helm-release modes)

The user supplies a layered configuration (defaults → embedded profile →
ConfigMap → ConfigFile → CLI flags). The Runner walks ordered Features
and either installs or destroys them. The destroy path is the inverse of
install.

Everything you build must serve that goal. If you find yourself adding a
generic abstraction that no current feature consumes, stop and re-check
the brief.

## 2. Working with the human

You are not authorised to invent decisions. When the next step has
multiple defensible answers — different APIs, different scope, an
ambiguous error to triage, a design trade-off you cannot resolve from
the docs in this repo — **stop and ask**. Silent guessing is the single
most expensive failure mode for agents on this code base.

### 2.1 When to ask (and when not to)

Ask when **any** of these are true:

- Two or more implementations would be reasonable and the existing
  code does not point at one over the other.
- A constraint in `AGENTS.md`, `SPECS.md`, `PORTING_PLAN.md` or
  `REMAINING.md` contradicts what the human just requested.
- An error message is ambiguous (multiple likely root causes) and the
  fixes are not symmetric in risk.
- The change would touch `retired/`, `.github/workflows/`, secrets,
  `go.mod`, `flake.nix`, `Dockerfile`, or anything else that affects
  shared state outside your local working copy.

Do **not** ask when the choice is a mechanical refactor, a docstring
fix, a test that follows an obvious pattern from a sibling file, or
when a single rule in `AGENTS.md` already resolves the ambiguity.

### 2.2 Format of the question

Keep it tight. The human reads dozens of agent questions a day.

1. **One-sentence problem statement** in plain prose. State the
   observable fact, the file + line if relevant, and what the agent
   needs to proceed. No history, no apologies.
2. **Up to three numbered options.** Each option is one sentence
   describing what you would do and one sentence on the trade-off.
   Never more than three; if you cannot narrow it to three, the
   options are not orthogonal enough — go back and group them.
3. **Your recommendation**, marked as such. If you cannot recommend,
   say "no recommendation, needs human judgement". The recommendation
   is not a vote of confidence; it is the agent's best guess so the
   human knows what they would get from a default reply.

Template:

```
**Problem**: <one sentence, factual>

1. <option A: action> — <trade-off>
2. <option B: action> — <trade-off>
3. <option C: action> — <trade-off>

**Recommended**: <number>, because <one clause>.
```

### 2.3 How to read what the human says back

Every human reply falls into one of three categories. Identify the
category first; the right next action depends on it.

#### (a) Error output from the application or CI

Triggers: pasted stack trace, compiler output, `make`/`nix`/`docker`
exit codes, GitHub Actions log fragments, kubectl error text.

What to do:

- Treat the paste as **evidence**, not as a brief. Do not "interpret"
  it into prose; quote the relevant line back when discussing it.
- Localise the failure to a specific file, line and call site before
  proposing a fix.
- Check whether `TEST_PLAN.md` already lists this finding from a
  previous iteration; if so, link to it rather than re-discovering it.
- If the root cause is uncertain, treat the situation as §2.1 ("Ask")
  and offer up to three diagnoses with the cheapest verification step
  per diagnosis.
- Only patch after the cause is named. Do not "fix and hope".

#### (b) Half-refined feature or change request

Triggers: "we should add", "it would be nice if", "make it possible
to", "I want a flag for", a one-paragraph user story with no
acceptance criteria.

What to do:

- Resist the urge to implement. The request is intentionally
  under-specified.
- Ask **targeted** questions in three groups, in this order, stopping
  as soon as you have a viable plan:
  1. **Scope**: what is in, what is out. Who is the user. What
     happens to existing config / CLI / API surface.
  2. **Shape**: what the public surface looks like — flag name, YAML
     key, struct field — and where the responsibility lives
     (which package, which existing interface).
  3. **Done criteria**: what tests demonstrate it works; what
     telemetry, log line or visible behaviour signals success in
     production.
- Each question is closed-form when possible ("Do you want X or Y?")
  not open-ended ("How do you want this to work?"). Open-ended
  questions are slow and produce vague answers.
- Wait for answers before writing code. If you need to write code to
  illustrate a question, mark it clearly as a sketch.

#### (c) Ordinary project-related request

Triggers: "rename this", "run the tests", "update the docs", "add a
case for X to the existing table test", concrete task IDs from
`REMAINING.md`.

What to do:

- Proceed directly. Confirm the scope back in one sentence ("I am
  about to do X in file Y, expecting Z to change"), then execute.
- Apply §4 (House style) and §6 (REMAINING.md workflow) as usual.
- If, mid-task, the request turns out to be category (b) in disguise
  (the scope opens up, a contradiction surfaces), pause and switch to
  §2.2.

### 2.4 Quick sanity check

Before sending a reply, run this checklist in your head:

- Did I classify the input as (a), (b) or (c)?
- If (b) or "ask", did I keep it to ≤ 3 options or ≤ 3 questions?
- Is every option / question one sentence?
- Did I quote the relevant evidence (a) or constraint (b) verbatim?
- Did I say what I would do by default if the human just replies
  "you decide"?

If any answer is no, fix it before sending.

## 3. Boot sequence (first thing every agent reads after §2)

1. **This file**, top to bottom.
2. [`README.md`](README.md) — what the project is from a user's POV.
3. [`PORTING_PLAN.md`](PORTING_PLAN.md) — the migration plan from
   Groovy. Most architectural decisions are justified there.
4. [`SPECS.md`](SPECS.md) — per-adapter design specs. If you touch an
   adapter (`internal/k8s`, `internal/jenkins`, …), the matching spec
   section is the source of truth for its public API.
5. [`REMAINING.md`](REMAINING.md) — the live to-do list. Pick a task
   from there or, with the human's agreement, append a new one.
6. [`TEST_PLAN.md`](TEST_PLAN.md) — every prior agent's toolchain
   run lives here. Read at least the latest iteration to know what is
   green right now.
7. [`BUILD.md`](BUILD.md) — how to build, test and what to expect on a
   first build.
8. Read the **package doc comment** of each package you intend to
   change. Every package starts with `// Package x …` that explains
   why it exists and what it deliberately does *not* do.

If a Sub-Agent is dispatched, that Sub-Agent is bound by **the same
order**. Pass these paths in its brief verbatim — never let it skip step
3 or 4.

## 4. House style

### 4.1 Module & layout

- Module path: `github.com/cloudogu/gitops-playground/go` (declared in
  `go.mod`, do NOT rename).
- One concern per package. New packages go under `internal/`. A package
  is allowed to exist when:
  - it owns a coherent surface that is plausibly mockable; OR
  - removing it would force two unrelated callers to duplicate logic.
- Public packages (under `pkg/`) are reserved. Do not create them
  without explicit human sign-off.

### 4.2 Naming

- Files: lowercase, snake-free. Use `helm_strategy.go`, not
  `HelmStrategy.go` and not `helmstrategy.go`.
- Types: `Client`, `Service`, `Feature`, never `MyClientStruct`.
- Constants: `defaultRegistryPort`, exported as `DefaultRegistryPort`
  only when callers need them.
- A struct never carries `Manager`, `Helper` or `Utility` in the name.
  If it does, the abstraction is wrong.

### 4.3 Error handling

- `return fmt.Errorf("doing X for Y/%s: %w", id, err)`. Always wrap with
  `%w` so callers can `errors.Is` / `errors.As`.
- A function either returns an error or panics. **No silent swallow.**
- `errors.New` is acceptable for sentinel errors *defined at package
  level* (`var ErrNothingToCommit = errors.New("…")`); inline strings
  should be `fmt.Errorf`.

### 4.4 Logging

- Use `log/slog` via the global default logger (`slog.Info`,
  `slog.Debug`, …). Levels live in `internal/log` (Info, Debug, Trace).
- Structured attrs, not `fmt.Sprintf`:
  - good: `slog.Info("installing feature", "name", f.Name())`
  - bad : `slog.Info(fmt.Sprintf("installing feature %s", f.Name()))`
- `slog.Debug` for actionable engineering detail; `Trace` for raw
  command/HTTP echo. Production output runs at Info.

### 4.5 Context

- Every method that can block on I/O takes `ctx context.Context` as the
  first argument. **No method stores a context as a field.**
- Pass `cmd.Context()` from Cobra into the runner, the runner into
  features, features into adapters.

### 4.6 Concurrency

- Default to single goroutine; introduce concurrency only when it
  changes wall-clock latency materially.
- When you do, never call user-supplied callbacks from multiple
  goroutines without documenting it.

### 4.7 Tests

- Table-driven, with `t.Run(tc.name, …)` so failures point at one row.
- One test file per source file: `foo.go` ↔ `foo_test.go`.
- Use the standard library plus the fakes the adapters already provide:
  - K8s: `k8s.io/client-go/kubernetes/fake`,
    `k8s.io/client-go/dynamic/fake`
  - HTTP: `net/http/httptest`
  - Git: in-process temp dirs (see `internal/git/repo_test.go`)
- No reliance on a live cluster or external service in unit tests. End-
  to-end tests go in `internal/runner/*_e2e_test.go` with the build tag
  `e2e`.
- Race detector: `make test` already runs `go test -race -count=1`.

### 4.8 Forbidden patterns

- **No reflection** to call optional methods. Use small interfaces and
  type assertions instead (see `internal/feature.PreConfigInit`).
- **No globals** other than constants and package-level loggers. State
  lives on structs.
- **No `init()`** that does anything except register a `flag` or an
  `encoding`. No network / disk in `init()`.
- **No `go.mod` edits without `go mod tidy`.** If you need a new
  dependency, add it via `go get` and run `go mod tidy`, then commit
  both files in the same patch.
- **No kubectl / helm shell-outs hidden inside K8s adapter code.** The
  K8s adapter uses `client-go`. Helm CLI is a deliberate surface
  (`internal/helm`), invoked from the deployment strategy only.
- **Never widen TLS-insecure beyond `cfg.Application.Insecure == true`.**
  The Groovy original disabled hostname verification unconditionally;
  that bug was fixed and stays fixed.

### 4.9 Cross-package dependencies

- A package's interface lives in the **consumer**, not the producer.
  Example: `internal/feature/image_pull.go` defines
  `ImagePullSecretCreator` — `internal/k8s` does not.
- This keeps `internal/k8s` mockable without importing `internal/feature`.

## 5. Documentation duties

Every change you make must keep the doc set consistent. The doc set is:

| File | Purpose | When to edit |
| --- | --- | --- |
| `README.md` | User-facing quick start | Public surface changed (flag, install command, docker tag) |
| `AGENTS.md` (this) | Agent contract | A coding rule or workflow was added/changed by humans |
| `PORTING_PLAN.md` | Architecture journal | A phase landed or a phase was added |
| `SPECS.md` | Per-adapter contracts | The public API of any `internal/<adapter>` changed |
| `BUILD.md` | First-build notes | Build / test / image instructions changed |
| `TEST_PLAN.md` | Iteration log of every real toolchain run | **Append a new entry after every iteration that ran the build/test pipeline** |
| `REMAINING.md` | Live to-do list | **After every successful change** (see §6) |
| `retired/**` | Archived Groovy | Never |

### 5.1 In-source docs

- Every package starts with a multi-line `// Package x` block that
  states what it does and what it deliberately does **not** do (the
  exclusion list catches scope creep).
- Every exported identifier has a godoc that begins with its own name.
- Comments inside a function explain **why**, not **what**. If the
  reader wonders “why this dance?” you owe them a comment.
- Bug-fix comments may reference the Groovy original by file name (no
  line numbers — they rot): `// fixes silent finalizer drop in
  ArgoCDDestructionHandler.groovy`.

### 5.2 Architectural Decision Records

When you make a non-trivial design decision that is not obvious from
reading the code, capture it as a short ADR-style block in `SPECS.md`
under the affected adapter or in the package's doc.go. ADRs **belong in
the repo, not in the commit message**, because commits get squashed.

## 6. REMAINING.md — the to-do board

`REMAINING.md` is the live agreement between you, the next agent and
the human. It is **not** a commit log.

### 6.1 When you start a task

1. Find or add the task in `REMAINING.md`. Append your agent name +
   timestamp in parentheses, e.g.
   `*(in-progress, claude-4.7, 2026-06-11)*`.
2. Reference the task ID in your commit message:
   `feat(content): implement COPY repo mode (REMAINING #1.4)`.
3. Branch off `main` (or the current feature branch the human points
   you to). Never commit to `main` directly.

### 6.2 When you finish a task

1. Mark it as resolved in `REMAINING.md`:
   - For small items, **strike through** the entry and add a
     `→ done in <commit-sha>` note.
   - For big buckets, replace the entry with a `done.<short-name>`
     subsection so future readers know which patches discharged it.
2. If you discovered follow-up work while solving the task, **append**
   it to `REMAINING.md` at the right priority bucket. Do not silently
   bury it in a TODO comment.
3. Re-read the priority ordering at the bottom of `REMAINING.md`. If
   your change unblocked something, move it up.

### 6.3 When you abandon a task

Update the entry with the blocker (`*blocked: needs human decision on
SCM-Manager URL shape*`) and stop. Do not push half-finished code with
"TODO: continue later" comments.

## 7. Commit / branch / PR workflow

1. **Branches**: `feature/<topic>` or `fix/<topic>`. Long-running
   tracks may use `track/<name>` (e.g. `track/content-loader`).
2. **Conventional commits**:
   `feat(scope): subject`, `fix(scope): subject`, `docs(scope): subject`,
   `build`, `test`, `refactor`, `chore`. Subject is imperative.
3. **One logical change per commit.** A bug fix and a documentation
   update of the same bug can ride together; a bug fix and an unrelated
   refactor cannot.
4. **Tests in the same commit** as the code they cover. Reviewers (human
   or agent) should see the code + the test land together.
5. **`make check` (or `nix flake check`) must be green** before you push.
   If you cannot run them (no Go toolchain), say so explicitly in your
   final report; do not pretend.
6. PRs target `main` and reference REMAINING.md task IDs in the
   description.
7. **Co-author trailer is mandatory for AI-authored commits.**
   Every commit you create — solo or assisting the human — ends with a
   `Co-Authored-By` trailer that names the model. This is the audit
   trail: a reader who sees a commit must be able to tell whether a
   human, an agent, or both produced it.

   Format:

   ```
   <commit message body>

   Co-Authored-By: <Model name and version> <noreply@anthropic.com>
   ```

   Concrete examples (use the one that matches your runtime):

   ```
   Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
   Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
   Co-Authored-By: GPT-5 Codex <noreply@openai.com>
   ```

   Rules:
   - Always use a HEREDOC for the commit message so the trailer keeps
     its own line.
   - If multiple agents contributed (e.g. a sub-agent wrote part of
     the patch), add one `Co-Authored-By` line per agent, in the order
     the work happened.
   - Never use a real human's address for the agent trailer. Stick to
     the no-reply addresses above; reviewers and analytics tooling
     depend on them.
   - This rule does NOT replace the human's authorship. `git
     commit --author` stays at the human's identity for their machine;
     the trailer is additive.

## 8. Verifying your work

In rough order of cost:

1. `make vet` — fast, catches the easy mistakes.
2. `make test` — race-tested, single iteration. < 30 s for the current
   suite.
3. `make build` — full binary build with version stamping.
4. `nix flake check` — reproducible build + golangci-lint.
5. `docker buildx build --platform linux/amd64,linux/arm64 -t gop:dev .`
   — final image. Only run before a release commit; skipped in normal
   iteration.

If you do not have a Go toolchain (frequent for sandboxed agents):
**say so loudly in your final report** and list every assumption you
made about types / signatures. Do not claim a clean build.

## 9. When to dispatch a Sub-Agent

Sub-Agents are leverage, not a default. Use one when **all** of:

- The task is self-contained (a single package or a tight pair).
- The task is read- and write-bounded by a few hundred lines.
- You can write a brief that is unambiguous about the API contract.

A good Sub-Agent brief contains:

1. The exact file paths to read first (this AGENTS.md + the relevant
   `SPECS.md` section + the package's existing doc.go).
2. A unique, narrowly scoped goal ("port X.groovy to Y.go using …").
3. **Hard constraints** ("do not touch `go.mod`", "do not change files
   outside `internal/foo`").
4. The verification path you expect the Sub-Agent to follow.
5. A line item asking the Sub-Agent to call out every assumption it
   made because it could not run `go build`.

When the Sub-Agent reports back:

- Spot-check the imports of one file per package it changed.
- Run `go vet` and `go test` on the affected package, even if the
  Sub-Agent claimed success. **Trust but verify**.
- Update `REMAINING.md` as if you had done the work yourself.

## 10. Common pitfalls (learned the hard way)

1. **`scmmanager.Config` does not carry credentials.** They flow
   through the `httpx.BasicAuth` transport. Reflexively adding
   `Username`/`Password` fields breaks the wire-graph in confusing ways.
2. **`k8s.Client.ApplyDockerConfigSecret` takes `namespace` before
   `name`.** Easy to flip.
3. **`PatchJSONMerge`, not `PatchTypeMerge`.** The wrong constant
   silently compiles and silently produces incorrect patches at runtime.
4. **CLI flags with the same default as a config value** are not
   detected as "user changed it" by the simple comparison in
   `internal/cli/pipeline.applyCLIOverrides`. If a feature depends on
   that distinction, use `cobra.Command.Flags().Changed(name)`
   explicitly. (Tracked in REMAINING.md P3.)
5. **`embed.FS` paths are case-sensitive and relative to the embedding
   file.** `//go:embed profiles/application-*.yaml` only picks up
   files that physically exist when `go build` runs; renames must be
   reflected immediately.
6. **`yaml.v3` replaces slices and maps on Unmarshal, it does not
   merge them.** This is by design and matches the Groovy
   `deepMerge` semantics; do not "fix" it.

## 11. Bug-fix hygiene (carried over from the rewrite)

The Go port deliberately fixed twelve documented bugs from the Groovy
original. They are listed in `PORTING_PLAN.md` §5. **Do not regress
them.** In particular:

- TLS insecure is opt-in (`cfg.Application.Insecure == true`) and
  scoped to the offending client. Never global.
- Pipe-style subprocess invocations go through `internal/exec.Pipe`,
  not hand-rolled goroutines.
- Optional hooks (`PreConfigInit`, `PostConfigInit`) are interfaces, not
  reflection.
- ImagePull-secret credential precedence is
  `proxy → readOnly → primary`. Centralised in
  `feature.EnsureProxyRegistryPullSecret`.

If a change makes one of these look optional, it is wrong.

## 12. What good looks like

A model-citizen PR for this repo:

- Tells a story in three to five commits.
- Each commit subject names the scope and the verb.
- The diff touches one or two packages, one feature, one obvious test.
- `REMAINING.md` shows one task struck through, possibly one new task
  appended.
- The PR description names the task ID, lists the verification it ran,
  flags every assumption it could not verify, and links to any external
  doc the agent had to consult.

That is the bar. Aim for it on every change.

---

### Quick reference

- Module path: `github.com/cloudogu/gitops-playground/go`
- Entry point: `./cmd/gop`
- Go version: 1.22+
- Build: `make build` or `nix build`
- Test: `make test` or `nix flake check`
- Container: `docker buildx build .`
- Live to-dos: `REMAINING.md`
- Architecture: `PORTING_PLAN.md` + `SPECS.md`
- Legacy: `retired/`
