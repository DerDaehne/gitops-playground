package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// Note on test design:
//
// The Service implementation uses go-git's plain (on-disk) backend, so
// all tests operate on directories returned by t.TempDir(). This keeps
// the test setup symmetrical with what the production code does. The
// SPECS hint at using go-billy.v5/memfs + storage/memory; in practice
// that requires a parallel implementation of Service against the memory
// backend, which is out of scope for the initial port. Once a feature
// needs that test mode, it can be added by exposing the storage backend
// behind an option on NewService.

func newSvc(t *testing.T) Service {
	t.Helper()
	return NewService()
}

func mustInit(t *testing.T, dir string) *Repo {
	t.Helper()
	r, err := newSvc(t).Init(context.Background(), InitOptions{
		Dir:    dir,
		Author: Identity{Name: "Tester", Email: "tester@example.com"},
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	return r
}

func writeFile(t *testing.T, dir, rel, contents string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestInitCreatesRepository(t *testing.T) {
	dir := t.TempDir()
	r := mustInit(t, dir)
	if r.Dir != dir {
		t.Fatalf("dir: got %q want %q", r.Dir, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf("expected .git directory, got: %v", err)
	}

	// A second Open should succeed.
	if _, err := newSvc(t).Open(dir, Auth{}, Identity{}, false); err != nil {
		t.Fatalf("open after init: %v", err)
	}
}

func TestInitRegistersRemote(t *testing.T) {
	dir := t.TempDir()
	svc := newSvc(t)
	r, err := svc.Init(context.Background(), InitOptions{
		Dir:       dir,
		Author:    Identity{Name: "Tester", Email: "t@example.com"},
		RemoteURL: "https://example.com/repo.git",
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if r.RemoteURL != "https://example.com/repo.git" {
		t.Fatalf("remote url: got %q", r.RemoteURL)
	}

	// Reopen and confirm the remote was persisted.
	r2, err := svc.Open(dir, Auth{}, Identity{}, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if r2.RemoteURL != "https://example.com/repo.git" {
		t.Fatalf("persisted remote: got %q", r2.RemoteURL)
	}
}

func TestCommitStagesAndRecords(t *testing.T) {
	dir := t.TempDir()
	r := mustInit(t, dir)
	writeFile(t, dir, "hello.txt", "hello world")

	if err := newSvc(t).Commit(r, "initial", CommitOptions{}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// A follow-up commit with no changes must report ErrNothingToCommit.
	err := newSvc(t).Commit(r, "noop", CommitOptions{})
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("expected ErrNothingToCommit, got %v", err)
	}
}

func TestCommitWithTag(t *testing.T) {
	dir := t.TempDir()
	r := mustInit(t, dir)
	writeFile(t, dir, "f.txt", "a")
	if err := newSvc(t).Commit(r, "first", CommitOptions{Tag: "v1"}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Re-applying the tag on a new commit should succeed thanks to the
	// idempotent delete-then-create behaviour.
	writeFile(t, dir, "f.txt", "b")
	if err := newSvc(t).Commit(r, "second", CommitOptions{Tag: "v1"}); err != nil {
		t.Fatalf("second commit with same tag: %v", err)
	}
}

func TestReadFileAtRef(t *testing.T) {
	dir := t.TempDir()
	r := mustInit(t, dir)
	writeFile(t, dir, "answer.txt", "42")
	if err := newSvc(t).Commit(r, "answer", CommitOptions{}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	data, err := newSvc(t).ReadFile(r, "HEAD", "answer.txt")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "42" {
		t.Fatalf("contents: got %q want %q", data, "42")
	}

	// Unknown path resolves to an error.
	if _, err := newSvc(t).ReadFile(r, "HEAD", "does-not-exist"); err == nil {
		t.Fatalf("expected error for missing path")
	}
}

func TestCloneFromLocalFileURL(t *testing.T) {
	// Set up a source repository with one commit.
	src := t.TempDir()
	r := mustInit(t, src)
	writeFile(t, src, "readme.md", "# clone target")
	if err := newSvc(t).Commit(r, "seed", CommitOptions{}); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	// Clone it into a second directory using a file:// URL.
	dst := filepath.Join(t.TempDir(), "clone")
	url := "file://" + src
	cloned, err := newSvc(t).Clone(context.Background(), url, CloneOptions{
		Dir:    dst,
		Author: Identity{Name: "Tester", Email: "t@example.com"},
	})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if cloned.Dir != dst {
		t.Fatalf("clone dir: got %q want %q", cloned.Dir, dst)
	}
	if cloned.RemoteURL != url {
		t.Fatalf("clone remote: got %q want %q", cloned.RemoteURL, url)
	}

	// Cloned file content matches.
	data, err := os.ReadFile(filepath.Join(dst, "readme.md"))
	if err != nil {
		t.Fatalf("read cloned file: %v", err)
	}
	if string(data) != "# clone target" {
		t.Fatalf("clone contents: got %q", data)
	}
}

func TestListBranches(t *testing.T) {
	dir := t.TempDir()
	r := mustInit(t, dir)
	writeFile(t, dir, "a.txt", "a")
	if err := newSvc(t).Commit(r, "first", CommitOptions{}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	branches, err := newSvc(t).ListBranches(context.Background(), r)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	sort.Strings(branches)
	// go-git's default initial branch for PlainInit is "master".
	// We don't assert on the exact name (this differs across go-git
	// versions / config) but we want at least one branch after the
	// first commit.
	if len(branches) == 0 {
		t.Fatalf("expected at least one branch, got %v", branches)
	}
}

func TestBuildAuthEmpty(t *testing.T) {
	if got := buildAuth(Auth{}); got != nil {
		t.Fatalf("expected nil auth for empty creds, got %v", got)
	}
}

func TestBuildAuthPopulated(t *testing.T) {
	got := buildAuth(Auth{Username: "u", Password: "p"})
	if got == nil || got.Username != "u" || got.Password != "p" {
		t.Fatalf("auth mismatch: %v", got)
	}
}

func TestReferenceNameFormats(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"main", "refs/heads/main"},
		{"refs/tags/v1", "refs/tags/v1"},
		{"refs/heads/feature", "refs/heads/feature"},
	}
	for _, c := range cases {
		if got := string(referenceName(c.in)); got != c.want {
			t.Errorf("referenceName(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

// Compile-time check: NewService satisfies the Service interface.
var _ Service = NewService()

// Sanity check: the Service interface itself stays small and
// purely-Git-focused. This guard catches accidental drift towards SCM
// concerns in future edits.
func TestServiceInterfaceMethods(t *testing.T) {
	want := []string{
		"Checkout", "Clone", "Commit", "Init",
		"ListBranches", "Open", "Push", "ReadFile",
	}
	got := serviceMethodNames()
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Service methods drifted:\n got:  %v\n want: %v", got, want)
	}
}

func serviceMethodNames() []string {
	t := reflect.TypeOf((*Service)(nil)).Elem()
	out := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		out = append(out, t.Method(i).Name)
	}
	return out
}
