# `internal/content` & `internal/githandler` – External Dependencies

Both packages compose existing internal packages (`internal/git`,
`internal/scm`, `internal/template`, `internal/fsutil`,
`internal/config`, `internal/feature`) and the standard library.

## New direct modules

**None.** The transitive footprint stays the same as the rest of the Go
port. In particular:

- `log/slog` is part of the standard library (Go 1.22+, matches `go.mod`).
- No new third-party YAML/JSON/HTTP/Git dependencies are introduced.

`go_src/go.mod` does *not* need any change.

## What is intentionally NOT yet wired

The content Feature has two collaborator types that depend on packages still
to be ported in later phases. Each is marked with a `// TODO:` comment at
its call site:

| Collaborator | Package (planned) | Used for |
|---|---|---|
| Image-pull-secret service | `internal/feature.ImagePullSecretCreator` (already exists, just not injected here yet) | Per-namespace `proxy-registry` secrets, see `createImagePullSecrets` in Groovy. |
| Helm deployment strategy | `internal/deployment.Strategy` | `cfg.content.helmReleases` processing, see `helm_release.go` for the planned call shape. |
| K8s secret-backed credentials | `internal/k8s` | Resolves `credentials.secretName`/`secretNamespace` → username/password. Today `credentialsToAuth` honours only plain username/password. |

When those collaborators are added to `feature.Feature`-compatible
constructors, expose them as fields on `content.Feature` and uncomment the
two TODO blocks in `content.go` / `helm_release.go`.
