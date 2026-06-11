package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// service is the default Service implementation. It is intentionally
// stateless – each operation re-opens the on-disk repository to avoid
// the memoised Git instance the Groovy code carried around.
type service struct{}

// NewService returns the default Service backed by go-git's plain
// (on-disk) storage.
func NewService() Service { return &service{} }

// Clone implements Service.
func (s *service) Clone(ctx context.Context, url string, opts CloneOptions) (*Repo, error) {
	if opts.Dir == "" {
		return nil, errors.New("git: clone requires a target directory")
	}
	cloneOpts := &gogit.CloneOptions{
		URL:             url,
		Auth:            buildAuth(opts.Auth),
		InsecureSkipTLS: opts.Insecure,
		SingleBranch:    opts.SingleBranch,
		Depth:           opts.Depth,
	}
	if opts.Ref != "" {
		cloneOpts.ReferenceName = referenceName(opts.Ref)
	}
	if _, err := gogit.PlainCloneContext(ctx, opts.Dir, false, cloneOpts); err != nil {
		return nil, fmt.Errorf("git: clone %s: %w", url, err)
	}
	return &Repo{
		Dir:       opts.Dir,
		Auth:      opts.Auth,
		Author:    opts.Author,
		Insecure:  opts.Insecure,
		RemoteURL: url,
	}, nil
}

// Init implements Service.
func (s *service) Init(ctx context.Context, opts InitOptions) (*Repo, error) {
	if opts.Dir == "" {
		return nil, errors.New("git: init requires a target directory")
	}
	repo, err := gogit.PlainInit(opts.Dir, opts.Bare)
	if err != nil {
		return nil, fmt.Errorf("git: init %s: %w", opts.Dir, err)
	}
	if opts.RemoteURL != "" {
		if _, err := repo.CreateRemote(&config.RemoteConfig{
			Name: "origin",
			URLs: []string{opts.RemoteURL},
		}); err != nil {
			return nil, fmt.Errorf("git: register origin: %w", err)
		}
	}
	return &Repo{
		Dir:       opts.Dir,
		Auth:      opts.Auth,
		Author:    opts.Author,
		Insecure:  opts.Insecure,
		RemoteURL: opts.RemoteURL,
	}, nil
}

// Open implements Service.
func (s *service) Open(dir string, auth Auth, author Identity, insecure bool) (*Repo, error) {
	if dir == "" {
		return nil, errors.New("git: open requires a directory")
	}
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("git: open %s: %w", dir, err)
	}
	remoteURL := ""
	if r, err := repo.Remote("origin"); err == nil {
		if urls := r.Config().URLs; len(urls) > 0 {
			remoteURL = urls[0]
		}
	}
	return &Repo{
		Dir:       dir,
		Auth:      auth,
		Author:    author,
		Insecure:  insecure,
		RemoteURL: remoteURL,
	}, nil
}

// Commit implements Service.
func (s *service) Commit(r *Repo, message string, opts CommitOptions) error {
	if r == nil {
		return errors.New("git: commit on nil Repo")
	}
	repo, err := gogit.PlainOpen(r.Dir)
	if err != nil {
		return fmt.Errorf("git: open %s: %w", r.Dir, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("git: worktree: %w", err)
	}

	if len(opts.Paths) == 0 {
		// Mirror the Groovy "git add ." behaviour. AddWithOptions{All}
		// stages every change in the working tree, including deletes,
		// which plain Add(".") does not always handle in older go-git
		// versions.
		if err := wt.AddWithOptions(&gogit.AddOptions{All: true}); err != nil {
			return fmt.Errorf("git: add all: %w", err)
		}
	} else {
		for _, p := range opts.Paths {
			if _, err := wt.Add(p); err != nil {
				return fmt.Errorf("git: add %s: %w", p, err)
			}
		}
	}

	status, err := wt.Status()
	if err != nil {
		return fmt.Errorf("git: status: %w", err)
	}
	if status.IsClean() && !opts.AllowEmpty {
		return ErrNothingToCommit
	}

	sig := &object.Signature{
		Name:  r.Author.Name,
		Email: r.Author.Email,
		When:  time.Now(),
	}
	hash, err := wt.Commit(message, &gogit.CommitOptions{
		Author:            sig,
		Committer:         sig,
		AllowEmptyCommits: opts.AllowEmpty,
	})
	if err != nil {
		return fmt.Errorf("git: commit: %w", err)
	}

	if opts.Tag != "" {
		// Delete first for idempotence, matching the Groovy behaviour.
		_ = repo.DeleteTag(opts.Tag)
		if _, err := repo.CreateTag(opts.Tag, hash, nil); err != nil {
			return fmt.Errorf("git: tag %s: %w", opts.Tag, err)
		}
	}
	return nil
}

// Push implements Service.
func (s *service) Push(ctx context.Context, r *Repo, opts PushOptions) error {
	if r == nil {
		return errors.New("git: push on nil Repo")
	}
	if r.RemoteURL == "" {
		return errors.New("git: push requires Repo.RemoteURL")
	}
	repo, err := gogit.PlainOpen(r.Dir)
	if err != nil {
		return fmt.Errorf("git: open %s: %w", r.Dir, err)
	}

	refSpec := opts.RefSpec
	if refSpec == "" {
		refSpec = "HEAD:refs/heads/main"
	}

	pushOpts := &gogit.PushOptions{
		RemoteURL:       r.RemoteURL,
		Auth:            buildAuth(r.Auth),
		InsecureSkipTLS: r.Insecure,
		Force:           opts.Force,
		RefSpecs:        []config.RefSpec{config.RefSpec(refSpec)},
	}

	if err := repo.PushContext(ctx, pushOpts); err != nil {
		if errors.Is(err, gogit.NoErrAlreadyUpToDate) {
			// Treat as success – the Groovy code does the same (it
			// only checks for uncommitted changes locally and never
			// errors out on already-up-to-date pushes).
			return nil
		}
		return fmt.Errorf("git: push %s: %w", refSpec, err)
	}

	if opts.IncludeTags {
		tagPush := &gogit.PushOptions{
			RemoteURL:       r.RemoteURL,
			Auth:            buildAuth(r.Auth),
			InsecureSkipTLS: r.Insecure,
			Force:           opts.Force,
			RefSpecs:        []config.RefSpec{config.RefSpec("refs/tags/*:refs/tags/*")},
		}
		if err := repo.PushContext(ctx, tagPush); err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
			return fmt.Errorf("git: push tags: %w", err)
		}
	}
	return nil
}

// Checkout implements Service.
func (s *service) Checkout(ctx context.Context, r *Repo, ref string) error {
	if r == nil {
		return errors.New("git: checkout on nil Repo")
	}
	if ref == "" {
		return errors.New("git: checkout requires a ref")
	}
	repo, err := gogit.PlainOpen(r.Dir)
	if err != nil {
		return fmt.Errorf("git: open %s: %w", r.Dir, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("git: worktree: %w", err)
	}

	// Try ref as branch first, then tag, then commit hash.
	co := &gogit.CheckoutOptions{}
	if hash, err := repo.ResolveRevision(plumbing.Revision(ref)); err == nil {
		// If the ref exactly matches a known branch/tag name we use
		// the symbolic form so HEAD points at it; otherwise we
		// detach onto the hash.
		if branchRef, err := repo.Reference(plumbing.NewBranchReferenceName(ref), true); err == nil {
			co.Branch = branchRef.Name()
		} else if tagRef, err := repo.Reference(plumbing.NewTagReferenceName(ref), true); err == nil {
			co.Branch = tagRef.Name()
		} else {
			co.Hash = *hash
		}
	} else {
		// Fall back to interpreting ref as a branch name directly.
		co.Branch = plumbing.NewBranchReferenceName(ref)
	}
	if err := wt.Checkout(co); err != nil {
		return fmt.Errorf("git: checkout %s: %w", ref, err)
	}
	return nil
}

// ListBranches implements Service.
func (s *service) ListBranches(ctx context.Context, r *Repo) ([]string, error) {
	if r == nil {
		return nil, errors.New("git: list branches on nil Repo")
	}
	repo, err := gogit.PlainOpen(r.Dir)
	if err != nil {
		return nil, fmt.Errorf("git: open %s: %w", r.Dir, err)
	}
	iter, err := repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("git: branches: %w", err)
	}
	var out []string
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		out = append(out, ref.Name().Short())
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("git: iterate branches: %w", err)
	}
	return out, nil
}

// ReadFile implements Service.
func (s *service) ReadFile(r *Repo, ref, path string) ([]byte, error) {
	if r == nil {
		return nil, errors.New("git: read on nil Repo")
	}
	if ref == "" {
		return nil, errors.New("git: read requires a ref")
	}
	repo, err := gogit.PlainOpen(r.Dir)
	if err != nil {
		return nil, fmt.Errorf("git: open %s: %w", r.Dir, err)
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("git: resolve %s: %w", ref, err)
	}
	commit, err := repo.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("git: commit %s: %w", hash.String(), err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("git: tree: %w", err)
	}
	file, err := tree.File(path)
	if err != nil {
		return nil, fmt.Errorf("git: file %s@%s: %w", path, ref, err)
	}
	reader, err := file.Reader()
	if err != nil {
		return nil, fmt.Errorf("git: reader %s: %w", path, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("git: read %s: %w", path, err)
	}
	return data, nil
}

// buildAuth turns a plain Auth into the go-git BasicAuth that its HTTP
// transport understands. Returns nil for empty credentials so go-git
// falls back to ambient auth (e.g. credential helpers, SSH agents) when
// no credentials are configured.
func buildAuth(a Auth) *http.BasicAuth {
	if a.Username == "" && a.Password == "" {
		return nil
	}
	return &http.BasicAuth{Username: a.Username, Password: a.Password}
}

// referenceName converts a short ref into a fully-qualified one. If the
// caller already passes "refs/..." it is used as-is.
func referenceName(ref string) plumbing.ReferenceName {
	if len(ref) >= 5 && ref[:5] == "refs/" {
		return plumbing.ReferenceName(ref)
	}
	return plumbing.NewBranchReferenceName(ref)
}
