// Package helm wraps the `helm` CLI. It is the Go counterpart of
// infrastructure/helm/HelmClient.groovy.
//
// Differences vs. the Groovy original:
//
//   - Flags are passed in via the typed UpgradeOptions struct rather than a
//     Map<String, String>. That keeps the argument order deterministic
//     (important for both tests and reproducibility) and lets us model
//     repeated flags such as --values cleanly.
//   - Errors include the command's stderr; the Groovy client only logged it.
//   - All calls are driven by a context.Context so timeouts and
//     cancellation come from the caller, not a global constant.
//
// The wrapper itself does not know about charts, releases or Kubernetes
// objects - it just translates method calls into well-formed helm
// invocations and runs them through internal/exec.Runner so the binary
// can be stubbed out in tests.
package helm

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudogu/gitops-playground/go/internal/exec"
)

// binary is the executable name. Kept as a package variable so it stays a
// single point of change should we ever want to support `helm3` or a
// fully qualified path.
const binary = "helm"

// Client wraps the helm CLI.
//
// The zero value is not usable; callers must provide a Runner. Use
// New(runner) for the common case.
type Client struct {
	Runner exec.Runner
}

// New returns a Client backed by the given Runner. If runner is nil the
// production exec.Real implementation is used.
func New(runner exec.Runner) Client {
	if runner == nil {
		runner = exec.Real{}
	}
	return Client{Runner: runner}
}

// UpgradeOptions models the optional flags accepted by `helm upgrade` and
// `helm template`. Only non-zero fields contribute to the command line.
//
// The fields are serialised in a fixed order (Namespace, Version, Values,
// Set, ExtraArgs); this is intentional so tests can assert on the exact
// argv slice without sorting.
type UpgradeOptions struct {
	// Namespace becomes `--namespace <value>`.
	Namespace string
	// Version becomes `--version <value>`.
	Version string
	// Values is a list of value files; each entry becomes `--values <path>`.
	Values []string
	// Set is a map of --set key=value overrides. Keys are emitted in
	// lexicographic order so the resulting argv is deterministic.
	Set map[string]string
	// CreateNS controls whether `--create-namespace` is appended. It is
	// honoured by Upgrade; Template ignores it because `helm template`
	// rejects the flag.
	CreateNS bool
	// ExtraArgs are appended verbatim at the end of the command line for
	// flags that are not modelled above (e.g. `--wait`, `--atomic`).
	ExtraArgs []string
}

// AddRepo runs `helm repo add <name> <url>`.
func (c Client) AddRepo(ctx context.Context, name, url string) error {
	_, err := c.run(ctx, []string{"repo", "add", name, url})
	return err
}

// DependencyBuild runs `helm dependency build <path>`.
func (c Client) DependencyBuild(ctx context.Context, path string) error {
	_, err := c.run(ctx, []string{"dependency", "build", path})
	return err
}

// Upgrade runs `helm upgrade -i <release> <chartOrPath>` plus the flags
// derived from opts. CreateNS, if set, becomes `--create-namespace`.
func (c Client) Upgrade(ctx context.Context, release, chartOrPath string, opts UpgradeOptions) error {
	args := []string{"upgrade", "-i", release, chartOrPath}
	args = append(args, optionArgs(opts, true)...)
	_, err := c.run(ctx, args)
	return err
}

// Template runs `helm template <release> <chartOrPath>` and returns
// stdout (the rendered manifests). CreateNS is ignored because the
// `template` subcommand does not accept it.
func (c Client) Template(ctx context.Context, release, chartOrPath string, opts UpgradeOptions) (string, error) {
	args := []string{"template", release, chartOrPath}
	args = append(args, optionArgs(opts, false)...)
	out, err := c.run(ctx, args)
	if err != nil {
		return "", err
	}
	return out.Stdout, nil
}

// Uninstall runs `helm uninstall <release> --namespace <namespace>`.
func (c Client) Uninstall(ctx context.Context, release, namespace string) error {
	_, err := c.run(ctx, []string{"uninstall", release, "--namespace", namespace})
	return err
}

// run is the single choke point through which every helm invocation
// flows. It enriches CLI errors with stderr so that callers do not need
// to inspect Output themselves.
func (c Client) run(ctx context.Context, args []string) (exec.Output, error) {
	runner := c.Runner
	if runner == nil {
		runner = exec.Real{}
	}
	out, err := runner.Run(ctx, binary, args...)
	if err != nil {
		stderr := strings.TrimSpace(out.Stderr)
		if stderr == "" {
			return out, fmt.Errorf("helm %s: %w", strings.Join(args, " "), err)
		}
		return out, fmt.Errorf("helm %s: %w: %s",
			strings.Join(args, " "), err, stderr)
	}
	return out, nil
}

// optionArgs flattens an UpgradeOptions into argv form. allowCreateNS
// gates `--create-namespace` because `helm template` rejects it.
//
// The emission order is fixed:
//  1. --namespace
//  2. --version
//  3. --values   (one occurrence per file, preserving slice order)
//  4. --set      (keys sorted alphabetically for determinism)
//  5. --create-namespace
//  6. ExtraArgs  (verbatim)
func optionArgs(opts UpgradeOptions, allowCreateNS bool) []string {
	var args []string
	if opts.Namespace != "" {
		args = append(args, "--namespace", opts.Namespace)
	}
	if opts.Version != "" {
		args = append(args, "--version", opts.Version)
	}
	for _, v := range opts.Values {
		args = append(args, "--values", v)
	}
	if len(opts.Set) > 0 {
		keys := make([]string, 0, len(opts.Set))
		for k := range opts.Set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "--set", k+"="+opts.Set[k])
		}
	}
	if allowCreateNS && opts.CreateNS {
		args = append(args, "--create-namespace")
	}
	if len(opts.ExtraArgs) > 0 {
		args = append(args, opts.ExtraArgs...)
	}
	return args
}
