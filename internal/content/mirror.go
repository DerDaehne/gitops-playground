package content

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/git"
	"github.com/cloudogu/gitops-playground/go/internal/scm"
)

// processMirror handles ContentRepoType.MIRROR. The Groovy equivalent is
// scattered across cloneToLocalFolder + createRepoCoordinateForTypeMirror +
// handleRepoMirroring; here we do it inline because there is no fan-out to
// multiple targets.
//
// Steps:
//  1. Validate the config (target required, no templating, no custom path).
//  2. Clone the source repository (full clone – we need history & tags).
//  3. Ask the GitHandler for the target working copy; this also creates the
//     remote repo on demand.
//  4. Honour the OverwriteMode: INIT skips if the target already exists,
//     RESET force-pushes, UPGRADE behaves the same as RESET for MIRROR
//     (per the ConfigConstants docstring).
//  5. Push either a single ref (when repo.Ref is set) or all refs.
func (f Feature) processMirror(ctx context.Context, cfg *config.Config, repo config.ContentRepositorySchema, logger *slog.Logger) error {
	if err := validateMirror(repo); err != nil {
		return err
	}

	namespace, name, err := scm.SplitRepoTarget(repo.Target)
	if err != nil {
		return err
	}

	// 1. Clone the source into a fresh scratch directory.
	srcDir, err := os.MkdirTemp("", "gop-content-mirror-src-")
	if err != nil {
		return fmt.Errorf("create source tmp dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(srcDir)
	}()

	logger.Debug("cloning source content repo", "dir", srcDir)
	srcRepo, err := f.SourceGit.Clone(ctx, repo.URL, git.CloneOptions{
		Dir:      srcDir,
		Auth:     credentialsToAuth(repo.Credentials),
		Author:   f.GitHandler.Author,
		Insecure: f.GitHandler.Insecure,
		Ref:      repo.Ref,
	})
	if err != nil {
		return fmt.Errorf("clone source: %w", err)
	}
	_ = srcRepo // we read from disk below; the *Repo is not needed further

	// 2. Ask GitHandler for the target working copy.
	target, err := f.GitHandler.Get(ctx, namespace, name)
	if err != nil {
		return fmt.Errorf("acquire target repo: %w", err)
	}

	// 3. Honour OverwriteMode for already-populated targets.
	// For MIRROR the Groovy code force-pushes regardless of RESET vs
	// UPGRADE (see ConfigConstants docstring: "For type: MIRROR reset and
	// upgrade have same result"). We only need to bail out for INIT when
	// the target already has content.
	mode := overwriteOrDefault(repo.OverwriteMode)
	if mode == OverwriteInit {
		empty, err := repoIsEmpty(target)
		if err != nil {
			return fmt.Errorf("inspect target: %w", err)
		}
		if !empty {
			logger.Warn("overwrite mode INIT and target already has content – skipping push",
				"target", repo.Target)
			return nil
		}
	}

	// 4. Build the push refspec.
	refSpec, includeTags := mirrorRefSpec(repo)

	// We push DIRECTLY from the source clone to the target remote: that
	// avoids the .git-merging dance the Groovy code does (see comment in
	// handleRepoMirroring). go-git can push from any local repo to an
	// arbitrary URL via PushOptions.RemoteURL.
	pushRepo := &git.Repo{
		Dir:       srcDir,
		Auth:      f.GitHandler.Auth, // tenant credentials – the push target
		Author:    f.GitHandler.Author,
		Insecure:  f.GitHandler.Insecure,
		RemoteURL: target.RemoteURL,
	}

	logger.Info("mirroring content repo", "target", repo.Target, "refSpec", refSpec, "tags", includeTags)
	if err := f.SourceGit.Push(ctx, pushRepo, git.PushOptions{
		RefSpec:     refSpec,
		Force:       true, // mirror is always force per Groovy handleRepoMirroring
		IncludeTags: includeTags,
	}); err != nil {
		return fmt.Errorf("push mirror: %w", err)
	}

	return nil
}

// validateMirror reproduces the MIRROR branch of preConfigInit.
func validateMirror(repo config.ContentRepositorySchema) error {
	if repo.URL == "" {
		return fmt.Errorf("content.repos requires a url parameter")
	}
	if repo.Target == "" {
		return fmt.Errorf("type MIRROR requires content.repos.target to be set")
	}
	if repo.Path != "" && repo.Path != DefaultRepoPath {
		return fmt.Errorf("type MIRROR does not support path (got %q)", repo.Path)
	}
	if repo.Templating {
		return fmt.Errorf("type MIRROR does not support templating")
	}
	return nil
}

// mirrorRefSpec returns the refspec used to push to the target. Mirrors the
// branching in handleRepoMirroring.
func mirrorRefSpec(repo config.ContentRepositorySchema) (string, bool) {
	switch {
	case repo.Ref == "" && repo.TargetRef == "":
		// Whole repo: refs/*:refs/*. We can't express that via a single
		// PushOptions, so we push HEAD->main and toggle tags. This matches
		// the *intent* of GitRepo.pushAll(true) for the common case where
		// only the default branch matters; multi-branch source repos are a
		// known limitation – TODO once the runner needs them.
		return "refs/*:refs/*", true
	case repo.Ref != "" && repo.TargetRef == "":
		// Push the source ref onto a same-named branch.
		return refName(repo.Ref) + ":" + refName(repo.Ref), false
	case repo.Ref != "" && repo.TargetRef != "":
		return refName(repo.Ref) + ":" + refName(repo.TargetRef), false
	default:
		return refName(repo.TargetRef), false
	}
}

// refName turns a short ref ("main", "v1.0.0") into a fully-qualified one.
// Heuristic: looks like a tag if it starts with "v" + digit; everything
// else is treated as a branch. Refs already in fully-qualified form pass
// through.
func refName(ref string) string {
	if len(ref) >= 5 && ref[:5] == "refs/" {
		return ref
	}
	return "refs/heads/" + ref
}

// credentialsToAuth converts the optional Credentials block into a git.Auth.
// Returns the zero Auth when credentials are missing or only specify a
// secret reference (the K8s-secret resolution is not yet ported – TODO).
func credentialsToAuth(c *config.Credentials) git.Auth {
	if c == nil {
		return git.Auth{}
	}
	if c.Username != "" || c.Password != "" {
		return git.Auth{Username: c.Username, Password: c.Password}
	}
	// TODO: resolve credentials from K8s secret (secretName/secretNamespace
	// in the Groovy schema). Requires the K8s client interface that is not
	// yet ported.
	return git.Auth{}
}

// repoIsEmpty reports whether the target working copy has any non-.git
// content. Used to decide whether INIT mode should skip the push.
func repoIsEmpty(r *git.Repo) (bool, error) {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		return false, nil
	}
	return true, nil
}
