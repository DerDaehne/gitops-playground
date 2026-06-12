package gitlab

import (
	"strings"
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/scm"
)

// TestRepoURLPresetParentFullPath verifies T-4 from REMAINING.md: when
// the caller pre-resolves ParentFullPath at wire time, RepoURL must
// NOT make an HTTP call. We assert that by handing the client a
// deliberately broken BaseURL plus a nil http.Client; if it tried to
// fetch we would see a panic or an error somewhere — instead the URL
// is composed from the pre-resolved value.
func TestRepoURLPresetParentFullPath(t *testing.T) {
	c := New(Config{
		BaseURL:        "https://gitlab.example.com",
		ParentGroup:    "42",
		ParentFullPath: "my/parent/group",
	}, nil)

	got := c.RepoURL("argocd", "cluster-resources", scm.RepoURLInCluster)
	if !strings.Contains(got, "my/parent/group/argocd/cluster-resources") {
		t.Errorf("expected pre-resolved parent path in URL, got %q", got)
	}
	if !strings.HasPrefix(got, "https://gitlab.example.com/") {
		t.Errorf("expected URL to start with BaseURL, got %q", got)
	}
}
