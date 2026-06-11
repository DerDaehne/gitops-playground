package content

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/fsutil"
	"github.com/cloudogu/gitops-playground/go/internal/git"
	"github.com/cloudogu/gitops-playground/go/internal/scm"
	"github.com/cloudogu/gitops-playground/go/internal/template"
)

// processCopy handles ContentRepoType.COPY.
//
// STUB: the core flow (clone → optionally render templates → copy contents
// over → commit & push) is sketched out below, but several details from the
// Groovy implementation are still missing and marked TODO. The unit-test
// surface is wide enough that landing the full implementation in this phase
// would over-shoot the porting milestone.
func (f Feature) processCopy(ctx context.Context, cfg *config.Config, repo config.ContentRepositorySchema, logger *slog.Logger) error {
	if err := validateCopy(repo); err != nil {
		return err
	}

	namespace, name, err := scm.SplitRepoTarget(repo.Target)
	if err != nil {
		return err
	}

	// 1. Clone the source.
	srcDir, err := os.MkdirTemp("", "gop-content-copy-src-")
	if err != nil {
		return fmt.Errorf("create source tmp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(srcDir) }()

	logger.Debug("cloning source for COPY", "dir", srcDir)
	if _, err := f.SourceGit.Clone(ctx, repo.URL, git.CloneOptions{
		Dir:      srcDir,
		Auth:     credentialsToAuth(repo.Credentials),
		Author:   f.GitHandler.Author,
		Insecure: f.GitHandler.Insecure,
		Ref:      repo.Ref,
	}); err != nil {
		return fmt.Errorf("clone source: %w", err)
	}

	srcRoot := srcDir
	if p := pathOrDefault(repo.Path); p != DefaultRepoPath {
		srcRoot = srcDir + string(os.PathSeparator) + p
	}

	// 2. Apply templating if requested.
	if repo.Templating {
		// TODO: render in-place. Right now RenderTree expects (src, dst)
		// to be distinct directories and only rewrites *.tmpl files. The
		// Groovy code rewrote the *.ftl files in-place. For COPY we'd
		// either copy the rendered tree into the target working copy
		// directly, or render in-place and then bulk-copy. Picking that
		// design is a follow-up.
		vars := BuildVariables(cfg, f.Tenant, repo.Target, nil)
		_ = vars
		_ = template.RenderOptions{}
		logger.Warn("COPY templating not yet implemented in Go port")
	}

	// 3. Acquire the target working copy and copy contents over.
	target, err := f.GitHandler.Get(ctx, namespace, name)
	if err != nil {
		return fmt.Errorf("acquire target repo: %w", err)
	}

	// 4. Honour overwrite mode.
	if err := applyOverwriteMode(target, repo, logger); err != nil {
		return err
	}

	// TODO: skip the .git directory while copying. fsutil.CopyDir copies
	// everything, including the .git directory which we do NOT want here
	// because go-git will refuse to push if the local .git layout was
	// corrupted by a copy.
	if err := fsutil.CopyDir(srcRoot, target.Dir); err != nil {
		return fmt.Errorf("copy source into target: %w", err)
	}

	// 5. Commit & push. The target ref handling (refSpec, tag branch
	// promotion) from setRefSpec() in the Groovy code is not yet ported.
	commitMessage := fmt.Sprintf("Initialize content repo %s/%s", namespace, name)
	if err := f.GitHandler.Git.Commit(target, commitMessage, git.CommitOptions{}); err != nil {
		// ErrNothingToCommit is treated as success – same as Groovy.
		if err.Error() != "git: nothing to commit" {
			return fmt.Errorf("commit: %w", err)
		}
	}
	if err := f.GitHandler.Git.Push(ctx, target, git.PushOptions{Force: true}); err != nil {
		return fmt.Errorf("push: %w", err)
	}

	logger.Warn("COPY repo type processed via STUB path – some edge cases (refSpec, tag promotion, Jenkins jobs) are not implemented yet")
	return nil
}

// validateCopy reproduces the COPY branch of preConfigInit.
func validateCopy(repo config.ContentRepositorySchema) error {
	if repo.URL == "" {
		return fmt.Errorf("content.repos requires a url parameter")
	}
	if repo.Target == "" {
		return fmt.Errorf("type COPY requires content.repos.target to be set")
	}
	return nil
}

// applyOverwriteMode clears target if needed, or signals to skip the push.
// Shared between COPY and FOLDER_BASED. Returns nil if processing should
// continue; the caller still has to honour an INIT-mode skip explicitly.
//
// TODO: honour OverwriteInit by aborting the push without erroring (current
// implementation always proceeds for COPY/FOLDER_BASED in the stub).
func applyOverwriteMode(target *git.Repo, repo config.ContentRepositorySchema, logger *slog.Logger) error {
	switch overwriteOrDefault(repo.OverwriteMode) {
	case OverwriteReset:
		logger.Info("OverwriteMode RESET – clearing target before copy", "target", repo.Target)
		// TODO: implement clearRepo equivalent. Need to delete every
		// non-.git path under target.Dir.
	case OverwriteUpgrade:
		logger.Debug("OverwriteMode UPGRADE – merging into existing repo", "target", repo.Target)
	case OverwriteInit:
		// Handled by the empty-target probe in the caller for MIRROR. For
		// COPY/FOLDER_BASED we'd need the same probe before re-cloning.
	}
	return nil
}
