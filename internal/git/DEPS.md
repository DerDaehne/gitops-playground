# `internal/git` – External Dependencies

The integrator must add the following modules to `go_src/go.mod` before
this package can be built or tested.

## Required direct dependency

| Module | Version (pinned) | Why |
|--------|------------------|-----|
| `github.com/go-git/go-git/v5` | `v5.12.0` | Provides `PlainInit`, `PlainCloneContext`, `PlainOpen`, `Worktree`, `PushContext`, `CommitObject`, `ResolveRevision`, and the `plumbing/transport/http.BasicAuth` type used in `repo.go`. |

Add it with:

```sh
cd go_src
go get github.com/go-git/go-git/v5@v5.12.0
```

`v5.12.0` is the current stable line (released Mar 2024). It exposes the
`InsecureSkipTLS` field on `CloneOptions`, `PushOptions`, `FetchOptions`
and `PullOptions`, which we rely on to replace the JGit
`InsecureCredentialProvider` workaround. Anything from `v5.10.0` onward
should compile, but `v5.12.0` is the version this code was written
against.

## Transitive dependencies introduced by go-git

These will be added automatically by `go mod tidy`. They are listed here
so the integrator knows what to expect in the `// indirect` block:

- `github.com/go-git/gcfg`
- `github.com/go-git/go-billy/v5` – the filesystem abstraction. Not
  imported by `repo.go` directly, but required transitively. If a future
  test rewrites the implementation to use `memfs + storage/memory`, this
  module must be promoted to a direct dependency.
- `github.com/ProtonMail/go-crypto`
- `github.com/cloudflare/circl`
- `github.com/emirpasic/gods`
- `github.com/jbenet/go-context`
- `github.com/kevinburke/ssh_config`
- `github.com/pjbgf/sha1cd`
- `github.com/sergi/go-diff`
- `github.com/skeema/knownhosts`
- `github.com/xanzy/ssh-agent`
- `golang.org/x/crypto`
- `golang.org/x/net`
- `golang.org/x/sys`

## Steps for the integrator

```sh
cd go_src
go get github.com/go-git/go-git/v5@v5.12.0
go mod tidy
go test ./internal/git/...
```

If `go mod tidy` flags `go-billy/v5` as needed by tests (e.g. when the
suggested memfs-based path is added later), promote it with:

```sh
go get github.com/go-git/go-billy/v5@v5.5.0
```

## Notes

- `repo.go` imports only four go-git packages:
  - `github.com/go-git/go-git/v5`
  - `github.com/go-git/go-git/v5/config`
  - `github.com/go-git/go-git/v5/plumbing`
  - `github.com/go-git/go-git/v5/plumbing/object`
  - `github.com/go-git/go-git/v5/plumbing/transport/http`
- We deliberately do NOT import `plumbing/transport/ssh`. The Groovy code
  only ever cloned/pushed via HTTPS, and SSH support pulls a large set of
  crypto transitive deps. Add it when an SCM provider actually needs SSH.
