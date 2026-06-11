// Package git wraps the go-git library to provide the small Git surface
// that the rest of GOP needs: clone, init, commit, push, checkout, list
// branches, and read a file at a given ref.
//
// It is the Go counterpart of the Git-only parts of GitRepo.groovy.
//
// Separation of concerns vs. the Groovy original:
//
//   - GitRepo.groovy mixes Git operations (clone/commit/push) with SCM
//     provider operations (createRepositoryAndSetPermission, gitProvider.
//     repoUrl, gitProvider.getCredentials). In Go we cleanly split that
//     up: this package does ONLY Git. The SCM provider lives in
//     internal/scm and constructs Repo values with a pre-resolved clone
//     URL and an Auth struct.
//   - Also gone: the file/template helpers (writeFile, replaceTemplates,
//     copyDirectoryContents, clearRepo). Callers that need them use
//     internal/fsutil + internal/template against r.Dir.
//   - The InsecureCredentialProvider workaround for JGit becomes a single
//     boolean on the Repo / clone options. go-git's http transport
//     understands transport/http.AuthMethod + InsecureSkipTLS natively,
//     so the YesNo-prompt parsing the JGit helper had to do is gone.
//
// Test seam: all operations are accessible via the Service interface so
// callers can stub Git in their unit tests. The default implementation
// (NewService) is a thin wrapper over go-git that operates on a *Repo.
package git

import (
	"context"
	"errors"
)

// Repo identifies a working copy on disk plus the credentials and author
// identity to use when interacting with the remote. Construct it via
// Clone, Init, or Open.
type Repo struct {
	// Dir is the absolute path to the working directory.
	Dir string

	// Auth holds the credentials used for clone/push operations.
	Auth Auth

	// Author is used as both the commit author and committer.
	Author Identity

	// Insecure, when true, disables TLS verification for remote
	// interactions on this repository (equivalent to the JGit
	// InsecureCredentialProvider in the Groovy code).
	Insecure bool

	// RemoteURL is the resolved clone URL of the repository. The SCM
	// package fills this in; Git itself never touches the provider.
	RemoteURL string
}

// Auth carries plain user/password credentials. Token-based auth is
// expressed by sending the token as Password with a non-empty Username
// (any non-empty username works for most providers, "oauth2" or the
// account name for SCM-Manager / GitLab).
type Auth struct {
	Username string
	Password string
}

// Identity describes the Git author/committer.
type Identity struct {
	Name  string
	Email string
}

// CloneOptions controls how a remote repository is cloned to disk.
type CloneOptions struct {
	// Dir is the target directory on disk. Required.
	Dir string

	// Auth is the credentials used for the fetch.
	Auth Auth

	// Author becomes the default Identity on the returned *Repo.
	Author Identity

	// Insecure skips TLS verification for HTTPS remotes. Equivalent to
	// the JGit InsecureCredentialProvider in the original Groovy code.
	Insecure bool

	// Ref, if set, is checked out after the clone (branch or tag).
	Ref string

	// Depth, if > 0, performs a shallow clone.
	Depth int

	// SingleBranch, when true together with Ref, only fetches that branch.
	SingleBranch bool
}

// InitOptions controls how a fresh repository is created on disk.
type InitOptions struct {
	// Dir is the target directory on disk. Required.
	Dir string

	// Auth is stored on the returned *Repo for later push operations.
	Auth Auth

	// Author becomes the default Identity on the returned *Repo.
	Author Identity

	// Insecure mirrors CloneOptions.Insecure.
	Insecure bool

	// RemoteURL, when non-empty, is registered as the "origin" remote.
	RemoteURL string

	// Bare creates a bare repository (no working tree).
	Bare bool
}

// PushOptions describes a push.
type PushOptions struct {
	// RefSpec follows the regular Git syntax, e.g. "HEAD:refs/heads/main"
	// or "refs/*:refs/*". When empty, "HEAD:refs/heads/main" is used,
	// matching the Groovy GitRepo default.
	RefSpec string

	// Force enables a force-push.
	Force bool

	// IncludeTags, when true, pushes annotated/lightweight tags
	// alongside the configured RefSpec.
	IncludeTags bool
}

// CommitOptions tunes Commit beyond the message. Paths, when empty,
// stages every change in the working tree (the Groovy code does
// `git add .` unconditionally). When non-empty, only the listed paths
// are staged.
type CommitOptions struct {
	// Paths are file globs/paths to add before committing. Empty stages
	// everything in the working tree.
	Paths []string

	// Tag, when non-empty, attaches a tag to the new commit. Pre-existing
	// tags with the same name are removed first for idempotence (same
	// behaviour as GitRepo.commitAndPush in Groovy).
	Tag string

	// AllowEmpty, when true, creates the commit even if no files were
	// staged. By default Commit silently returns ErrNothingToCommit if
	// nothing changed, again matching the Groovy implementation.
	AllowEmpty bool
}

// ErrNothingToCommit is returned by Commit when there are no staged
// changes and CommitOptions.AllowEmpty is false.
var ErrNothingToCommit = errors.New("git: nothing to commit")

// Service abstracts Git operations so that callers can stub them in unit
// tests without bringing up go-git's billy filesystem.
//
// The default implementation (NewService) operates on the file system in
// r.Dir using go-git's plain backend.
type Service interface {
	// Clone fetches the repository at url into opts.Dir.
	Clone(ctx context.Context, url string, opts CloneOptions) (*Repo, error)

	// Init creates a fresh repository at opts.Dir.
	Init(ctx context.Context, opts InitOptions) (*Repo, error)

	// Open opens an existing repository previously created by Clone or
	// Init. The returned *Repo carries the supplied Auth / Author /
	// Insecure values; nothing on disk is changed.
	Open(dir string, auth Auth, author Identity, insecure bool) (*Repo, error)

	// Commit stages changes (per opts.Paths) and records a commit with
	// the given message. Returns ErrNothingToCommit when no changes are
	// staged and AllowEmpty is false.
	Commit(r *Repo, message string, opts CommitOptions) error

	// Push uploads refs to r.RemoteURL using r.Auth.
	Push(ctx context.Context, r *Repo, opts PushOptions) error

	// Checkout switches the working tree to ref (branch, tag, or commit).
	Checkout(ctx context.Context, r *Repo, ref string) error

	// ListBranches returns the short names of all local branches.
	ListBranches(ctx context.Context, r *Repo) ([]string, error)

	// ReadFile returns the contents of path at ref (branch, tag, or
	// commit). It does NOT require a checkout of ref.
	ReadFile(r *Repo, ref, path string) ([]byte, error)
}
