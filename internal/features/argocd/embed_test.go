package argocd

import (
	"io/fs"
	"testing"
)

// TestClusterResourcesFS_KnownFiles confirms that the embedded
// cluster-resources tree contains the entries the runner expects to
// project over the cloned repo. The list intentionally mixes a Helm
// chart artefact, an FTL template and the hidden .gitkeep so a
// regression in the go:embed directive (e.g. dropping the `all:`
// prefix) is caught here rather than at install time.
func TestClusterResourcesFS_KnownFiles(t *testing.T) {
	t.Parallel()

	tfs, err := ClusterResourcesFS()
	if err != nil {
		t.Fatalf("ClusterResourcesFS: %v", err)
	}

	cases := []string{
		"apps/argocd/argocd/Chart.yaml",
		"apps/argocd/argocd/values.ftl.yaml",
		"apps/argocd/argocd/templates/.gitkeep",
		"apps/argocd/argocd/templates/allow-namespaces.ftl.yaml",
		"apps/monitoring/templates/prometheus-stack-helm-values.ftl.yaml",
		"README.md",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			info, err := fs.Stat(tfs, name)
			if err != nil {
				t.Fatalf("fs.Stat(%q): %v", name, err)
			}
			if info.IsDir() {
				t.Fatalf("%q is a directory, expected file", name)
			}
			b, err := fs.ReadFile(tfs, name)
			if err != nil {
				t.Fatalf("fs.ReadFile(%q): %v", name, err)
			}
			// .gitkeep is legitimately empty; everything else must
			// carry at least a header.
			if name != "apps/argocd/argocd/templates/.gitkeep" && len(b) == 0 {
				t.Fatalf("%q is empty", name)
			}
		})
	}
}

// TestClusterResourcesFS_FileCount guards against an accidental drop
// of entries in the embed directive. The number matches `find
// internal/features/argocd/embed/cluster-resources -type f | wc -l` at
// the time WP-A1 landed; if the template tree legitimately grows the
// constant moves with it.
func TestClusterResourcesFS_FileCount(t *testing.T) {
	t.Parallel()

	tfs, err := ClusterResourcesFS()
	if err != nil {
		t.Fatalf("ClusterResourcesFS: %v", err)
	}

	const wantFiles = 43

	got := 0
	if err := fs.WalkDir(tfs, ".", func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			got++
		}
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}

	if got != wantFiles {
		t.Fatalf("embedded file count = %d, want %d", got, wantFiles)
	}
}
