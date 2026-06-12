package argocd

// assets.go ships the cluster-resources template tree inside the
// binary. The Groovy original read these files from the project
// working directory (argocd/cluster-resources/...); after the Phase 8
// retirement that tree lives only under retired/, which a packaged
// `gop` binary will never see. Embedding via go:embed mirrors the
// pattern already used by internal/profile (application-*.yaml) and
// lets `nix build` produce a self-contained binary.
//
// The `all:` prefix is deliberate: the chart includes an
// `apps/argocd/argocd/templates/.gitkeep` file. By default go:embed
// skips entries whose name starts with `.` or `_`; `all:` opts back
// in. Without it the umbrella chart's templates/ folder would silently
// be empty at runtime and `helm dependency build` would still happen
// to work but the resulting Helm release would miss the
// NetworkPolicy template.

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed all:embed/cluster-resources
var clusterResourcesFS embed.FS

// embedRoot is the prefix every entry in clusterResourcesFS carries.
// Stripped via fs.Sub in ClusterResourcesFS so callers operate on a
// clean root ("apps/...", "README.md", ...).
const embedRoot = "embed/cluster-resources"

// ClusterResourcesFS returns the embedded cluster-resources template
// tree rooted at "" so that, e.g., "apps/argocd/argocd/Chart.yaml" is
// reachable directly.
//
// The returned fs.FS is safe for concurrent use; embed.FS is
// effectively immutable.
func ClusterResourcesFS() (fs.FS, error) {
	sub, err := fs.Sub(clusterResourcesFS, embedRoot)
	if err != nil {
		return nil, fmt.Errorf("argocd: sub %q: %w", embedRoot, err)
	}
	return sub, nil
}
