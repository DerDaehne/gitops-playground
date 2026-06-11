package helm

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/exec"
)

// recordedCall captures one invocation of the fake Runner.
type recordedCall struct {
	Name string
	Args []string
}

// fakeRunner is a stub exec.Runner used by every test in this file. It
// records each call and returns the canned output/error sitting at the
// head of its queues. If the queues are empty an empty Output and nil
// error are returned.
type fakeRunner struct {
	calls   []recordedCall
	outputs []exec.Output
	errs    []error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (exec.Output, error) {
	f.calls = append(f.calls, recordedCall{Name: name, Args: append([]string(nil), args...)})
	var out exec.Output
	if len(f.outputs) > 0 {
		out = f.outputs[0]
		f.outputs = f.outputs[1:]
	}
	var err error
	if len(f.errs) > 0 {
		err = f.errs[0]
		f.errs = f.errs[1:]
	}
	return out, err
}

func (f *fakeRunner) RunCommand(ctx context.Context, c exec.Command) (exec.Output, error) {
	return f.Run(ctx, c.Name, c.Args...)
}

func (f *fakeRunner) Pipe(_ context.Context, _, _ exec.Command) (exec.Output, error) {
	return exec.Output{}, errors.New("fakeRunner.Pipe not implemented")
}

// lastCall returns the most recently recorded call or fails the test if
// the runner was never invoked.
func (f *fakeRunner) lastCall(t *testing.T) recordedCall {
	t.Helper()
	if len(f.calls) == 0 {
		t.Fatalf("no calls recorded on fakeRunner")
	}
	return f.calls[len(f.calls)-1]
}

func TestAddRepo(t *testing.T) {
	r := &fakeRunner{}
	c := New(r)

	if err := c.AddRepo(context.Background(), "argo", "https://argoproj.example/charts"); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	got := r.lastCall(t)
	if got.Name != "helm" {
		t.Errorf("binary: got %q, want %q", got.Name, "helm")
	}
	want := []string{"repo", "add", "argo", "https://argoproj.example/charts"}
	if !reflect.DeepEqual(got.Args, want) {
		t.Errorf("args mismatch:\n got: %v\nwant: %v", got.Args, want)
	}
}

func TestDependencyBuild(t *testing.T) {
	r := &fakeRunner{}
	c := New(r)

	if err := c.DependencyBuild(context.Background(), "/tmp/chart"); err != nil {
		t.Fatalf("DependencyBuild: %v", err)
	}
	want := []string{"dependency", "build", "/tmp/chart"}
	if got := r.lastCall(t).Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestUpgradeWithNamespaceVersionAndValues(t *testing.T) {
	r := &fakeRunner{}
	c := New(r)

	opts := UpgradeOptions{
		Namespace: "argocd",
		Version:   "5.2.1",
		Values:    []string{"values-a.yaml", "values-b.yaml"},
		Set:       map[string]string{"image.tag": "1.0", "image.repo": "ghcr.io/foo"},
		CreateNS:  true,
		ExtraArgs: []string{"--wait"},
	}
	if err := c.Upgrade(context.Background(), "argocd", "argo/argo-cd", opts); err != nil {
		t.Fatalf("Upgrade: %v", err)
	}

	// Argument order is part of the contract; assert exact slice equality.
	want := []string{
		"upgrade", "-i", "argocd", "argo/argo-cd",
		"--namespace", "argocd",
		"--version", "5.2.1",
		"--values", "values-a.yaml",
		"--values", "values-b.yaml",
		// --set keys are emitted in lexicographic order
		"--set", "image.repo=ghcr.io/foo",
		"--set", "image.tag=1.0",
		"--create-namespace",
		"--wait",
	}
	if got := r.lastCall(t).Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestUpgradeMinimal(t *testing.T) {
	r := &fakeRunner{}
	c := New(r)

	if err := c.Upgrade(context.Background(), "rel", "my/chart", UpgradeOptions{}); err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	want := []string{"upgrade", "-i", "rel", "my/chart"}
	if got := r.lastCall(t).Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestTemplateReturnsStdoutAndOmitsCreateNamespace(t *testing.T) {
	const rendered = "apiVersion: v1\nkind: ConfigMap\n"
	r := &fakeRunner{
		outputs: []exec.Output{{Stdout: rendered}},
	}
	c := New(r)

	opts := UpgradeOptions{
		Namespace: "foo",
		Values:    []string{"v.yaml"},
		// CreateNS must be ignored by Template.
		CreateNS: true,
	}
	got, err := c.Template(context.Background(), "rel", "my/chart", opts)
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	if got != rendered {
		t.Errorf("stdout: got %q, want %q", got, rendered)
	}
	wantArgs := []string{
		"template", "rel", "my/chart",
		"--namespace", "foo",
		"--values", "v.yaml",
	}
	if a := r.lastCall(t).Args; !reflect.DeepEqual(a, wantArgs) {
		t.Errorf("args mismatch:\n got: %v\nwant: %v", a, wantArgs)
	}
}

func TestUninstall(t *testing.T) {
	r := &fakeRunner{}
	c := New(r)

	if err := c.Uninstall(context.Background(), "argocd", "argocd"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	want := []string{"uninstall", "argocd", "--namespace", "argocd"}
	if got := r.lastCall(t).Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestErrorIncludesStderr(t *testing.T) {
	boom := errors.New("exit status 1")
	r := &fakeRunner{
		outputs: []exec.Output{{Stderr: "Error: chart not found"}},
		errs:    []error{boom},
	}
	c := New(r)

	err := c.Upgrade(context.Background(), "rel", "missing/chart", UpgradeOptions{})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error should wrap the runner error, got %v", err)
	}
	if !strings.Contains(err.Error(), "Error: chart not found") {
		t.Errorf("error should include stderr, got %q", err.Error())
	}
}

func TestErrorWithoutStderr(t *testing.T) {
	boom := errors.New("exit status 2")
	r := &fakeRunner{errs: []error{boom}}
	c := New(r)

	err := c.AddRepo(context.Background(), "x", "https://x")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error should wrap the runner error, got %v", err)
	}
	// No stderr means no trailing colon-separated stderr fragment.
	if strings.HasSuffix(err.Error(), ": ") {
		t.Errorf("error should not end with empty stderr fragment, got %q", err.Error())
	}
}

func TestTemplateErrorPropagatesAndReturnsEmptyString(t *testing.T) {
	boom := errors.New("exit status 1")
	r := &fakeRunner{
		outputs: []exec.Output{{Stdout: "partial output", Stderr: "render failed"}},
		errs:    []error{boom},
	}
	c := New(r)

	out, err := c.Template(context.Background(), "rel", "chart", UpgradeOptions{})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if out != "" {
		t.Errorf("on error Template should return empty string, got %q", out)
	}
}

func TestNewWithNilRunnerDoesNotPanic(t *testing.T) {
	// We cannot Run anything (helm may not exist), but constructing the
	// client and reaching for the runner field must work.
	c := New(nil)
	if c.Runner == nil {
		t.Fatalf("New(nil) should fall back to a non-nil Runner")
	}
}
