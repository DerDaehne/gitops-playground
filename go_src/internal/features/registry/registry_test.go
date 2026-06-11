package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
	"github.com/cloudogu/gitops-playground/go/internal/exec"
	"github.com/cloudogu/gitops-playground/go/internal/helm"
)

// --- test stubs ------------------------------------------------------------

// recordingRunner captures every exec invocation the helm CLI would make.
// It returns empty Output and nil error so the chart "deploys" cleanly.
type recordingRunner struct {
	commands [][]string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (exec.Output, error) {
	r.commands = append(r.commands, append([]string{name}, args...))
	return exec.Output{}, nil
}

func (r *recordingRunner) RunCommand(_ context.Context, c exec.Command) (exec.Output, error) {
	r.commands = append(r.commands, append([]string{c.Name}, c.Args...))
	return exec.Output{}, nil
}

func (r *recordingRunner) Pipe(_ context.Context, _, _ exec.Command) (exec.Output, error) {
	return exec.Output{}, nil
}

// nodePortCall records a single CreateServiceNodePort invocation.
type nodePortCall struct {
	name, tcp, nodePort, namespace string
}

type fakeNodePort struct {
	calls []nodePortCall
	err   error
}

func (f *fakeNodePort) CreateServiceNodePort(_ context.Context, name, tcp, nodePort, namespace string) error {
	f.calls = append(f.calls, nodePortCall{name, tcp, nodePort, namespace})
	return f.err
}

// --- tests -----------------------------------------------------------------

func TestMetadata(t *testing.T) {
	var f Feature
	if got, want := f.Name(), "registry"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if got, want := f.Order(), 40; got != want {
		t.Errorf("Order = %d, want %d", got, want)
	}
}

func TestIsEnabled(t *testing.T) {
	cases := []struct {
		name             string
		active, internal bool
		url              string
		want             bool
	}{
		{"inactive", false, true, "", false},
		{"active+internal", true, true, "", true},
		{"active+external (url set, internal=false)", true, false, "https://reg.example", false},
		{"active but internal=false without url", true, false, "", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.New()
			cfg.Registry.Active = tc.active
			cfg.Registry.Internal = tc.internal
			cfg.Registry.URL = tc.url
			if got := (Feature{}).IsEnabled(cfg); got != tc.want {
				t.Errorf("IsEnabled = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNamespace(t *testing.T) {
	cfg := config.New() // Internal = true by default
	cfg.Application.NamePrefix = "tenant-"
	if got, want := (Feature{}).Namespace(cfg), "tenant-registry"; got != want {
		t.Errorf("Namespace internal = %q, want %q", got, want)
	}

	cfg.Registry.Internal = false
	if got := (Feature{}).Namespace(cfg); got != "" {
		t.Errorf("Namespace external = %q, want empty", got)
	}
}

func TestBuildValues(t *testing.T) {
	v := buildValues()
	svc, ok := v["service"].(map[string]any)
	if !ok {
		t.Fatalf("service block missing or wrong type: %#v", v["service"])
	}
	if got, want := svc["type"], "NodePort"; got != want {
		t.Errorf("service.type = %v, want %v", got, want)
	}
	if got, want := svc["nodePort"], config.DefaultRegistryPort; got != want {
		t.Errorf("service.nodePort = %v, want %v", got, want)
	}
}

func TestInstall_DefaultPort_SkipsExtraService(t *testing.T) {
	cfg := config.New()
	cfg.Registry.Active = true
	cfg.Application.NamePrefix = "t-"

	runner := &recordingRunner{}
	np := &fakeNodePort{}
	f := Feature{
		Helm:     deployment.HelmStrategy{Client: ptrToClient(helm.New(runner))},
		NodePort: np,
	}

	if err := f.Install(context.Background(), cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if len(np.calls) != 0 {
		t.Errorf("CreateServiceNodePort must not be called at default port; got %d calls", len(np.calls))
	}
	if len(runner.commands) == 0 {
		t.Fatalf("expected at least one helm invocation")
	}
}

func TestInstall_CustomPort_CreatesAdditionalNodePort(t *testing.T) {
	cfg := config.New()
	cfg.Registry.Active = true
	cfg.Registry.InternalPort = 32769 // != DefaultRegistryPort
	cfg.Application.NamePrefix = "tenant-"

	runner := &recordingRunner{}
	np := &fakeNodePort{}
	f := Feature{
		Helm:     deployment.HelmStrategy{Client: ptrToClient(helm.New(runner))},
		NodePort: np,
	}

	if err := f.Install(context.Background(), cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if len(np.calls) != 1 {
		t.Fatalf("CreateServiceNodePort calls = %d, want 1", len(np.calls))
	}
	got := np.calls[0]
	want := nodePortCall{
		name:      "docker-registry-internal-port",
		tcp:       "5000:5000",
		nodePort:  "32769",
		namespace: "tenant-registry",
	}
	if got != want {
		t.Errorf("CreateServiceNodePort args = %+v, want %+v", got, want)
	}
}

func TestInstall_CustomPort_NoNodePortCreator_NoOp(t *testing.T) {
	cfg := config.New()
	cfg.Registry.Active = true
	cfg.Registry.InternalPort = 31000

	runner := &recordingRunner{}
	f := Feature{
		Helm:     deployment.HelmStrategy{Client: ptrToClient(helm.New(runner))},
		NodePort: nil, // explicitly absent – must NOT panic / error
	}

	if err := f.Install(context.Background(), cfg); err != nil {
		t.Fatalf("Install with nil NodePort: %v", err)
	}
}

func TestInstall_NodePortError_Propagates(t *testing.T) {
	cfg := config.New()
	cfg.Registry.Active = true
	cfg.Registry.InternalPort = 31000

	runner := &recordingRunner{}
	boom := errors.New("boom")
	np := &fakeNodePort{err: boom}
	f := Feature{
		Helm:     deployment.HelmStrategy{Client: ptrToClient(helm.New(runner))},
		NodePort: np,
	}

	err := f.Install(context.Background(), cfg)
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom error, got %v", err)
	}
}

// helm.New returns a value, but HelmStrategy holds a *helm.Client. Tiny
// helper to keep the test setup readable.
func ptrToClient(c helm.Client) *helm.Client { return &c }
