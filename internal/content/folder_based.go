package content

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

// processFolderBased handles ContentRepoType.FOLDER_BASED.
//
// STUB: not yet implemented. The Groovy code clones the source, then walks
// the (templated) directory tree as <namespace>/<name>, treating each leaf
// as its own COPY-style target. Once processCopy is fully wired (see the
// TODOs in copy.go) this routine should reduce to a fan-out loop over the
// leaves.
func (f Feature) processFolderBased(_ context.Context, _ *config.Config, repo config.ContentRepositorySchema, logger *slog.Logger) error {
	if err := validateFolderBased(repo); err != nil {
		return err
	}
	logger.Warn("FOLDER_BASED repo type not implemented in Go port yet",
		"url", repo.URL,
	)
	return fmt.Errorf("FOLDER_BASED repo type not implemented yet (url=%s)", repo.URL)
}

// validateFolderBased reproduces the FOLDER_BASED branch of preConfigInit.
func validateFolderBased(repo config.ContentRepositorySchema) error {
	if repo.URL == "" {
		return fmt.Errorf("content.repos requires a url parameter")
	}
	if repo.Target != "" {
		return fmt.Errorf("type FOLDER_BASED does not support target parameter (got %q)", repo.Target)
	}
	if repo.TargetRef != "" {
		return fmt.Errorf("type FOLDER_BASED does not support targetRef parameter (got %q)", repo.TargetRef)
	}
	return nil
}
