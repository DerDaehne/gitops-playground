// Package exec wraps os/exec to provide the small surface the rest of GOP
// needs. It is the Go counterpart of CommandExecutor.groovy.
//
// Differences vs. the Groovy original:
//
//   - Timeouts are driven by context, not a global PROCESS_TIMEOUT_MINUTES.
//   - The two-command pipe variant uses io.Pipe + explicit synchronisation
//     on each cmd.Wait, so the race the Groovy author flagged with
//     "concurrency 🤷" cannot happen.
//   - Trace output goes through slog.
package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"strings"

	"github.com/cloudogu/gitops-playground/go/internal/log"
)

// Output captures the result of a single command invocation.
type Output struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Command describes one process. Env is merged on top of the parent
// environment; pass nil to inherit unchanged.
type Command struct {
	Name string
	Args []string
	Env  map[string]string
	Dir  string
}

// Runner is the interface every caller depends on so tests can swap a stub.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Output, error)
	RunCommand(ctx context.Context, c Command) (Output, error)
	Pipe(ctx context.Context, first, second Command) (Output, error)
}

// Real is the production implementation backed by os/exec.
type Real struct{}

// Run executes name + args without environment overrides.
func (r Real) Run(ctx context.Context, name string, args ...string) (Output, error) {
	return r.RunCommand(ctx, Command{Name: name, Args: args})
}

// RunCommand executes a single Command.
func (r Real) RunCommand(ctx context.Context, c Command) (Output, error) {
	cmd := osexec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Env = buildEnv(c.Env)
	cmd.Dir = c.Dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Trace("exec", "cmd", c.Name, "args", c.Args)
	err := cmd.Run()
	out := Output{
		Stdout:   strings.TrimRight(stdout.String(), "\n"),
		Stderr:   strings.TrimRight(stderr.String(), "\n"),
		ExitCode: cmd.ProcessState.ExitCode(),
	}
	if err != nil {
		return out, fmt.Errorf("exec %s %s: %w (stderr: %s)",
			c.Name, strings.Join(c.Args, " "), err, out.Stderr)
	}
	return out, nil
}

// Pipe runs `first | second`, returning the second command's output.
// Both processes start in parallel; we Wait on the producer first, then
// the consumer. If either fails, the error captures both stderrs.
func (r Real) Pipe(ctx context.Context, first, second Command) (Output, error) {
	prod := osexec.CommandContext(ctx, first.Name, first.Args...)
	prod.Env = buildEnv(first.Env)
	prod.Dir = first.Dir

	cons := osexec.CommandContext(ctx, second.Name, second.Args...)
	cons.Env = buildEnv(second.Env)
	cons.Dir = second.Dir

	pipeR, pipeW := io.Pipe()
	prod.Stdout = pipeW
	cons.Stdin = pipeR

	var prodErr, consOut, consErr bytes.Buffer
	prod.Stderr = &prodErr
	cons.Stdout = &consOut
	cons.Stderr = &consErr

	if err := prod.Start(); err != nil {
		return Output{}, fmt.Errorf("starting %s: %w", first.Name, err)
	}
	if err := cons.Start(); err != nil {
		_ = prod.Process.Kill()
		_ = pipeW.Close()
		return Output{}, fmt.Errorf("starting %s: %w", second.Name, err)
	}

	prodWaitErr := prod.Wait()
	_ = pipeW.Close()
	consWaitErr := cons.Wait()

	out := Output{
		Stdout:   strings.TrimRight(consOut.String(), "\n"),
		Stderr:   strings.TrimRight(consErr.String(), "\n"),
		ExitCode: cons.ProcessState.ExitCode(),
	}
	if prodWaitErr != nil {
		return out, fmt.Errorf("pipe producer %s failed: %w (stderr: %s)",
			first.Name, prodWaitErr, strings.TrimRight(prodErr.String(), "\n"))
	}
	if consWaitErr != nil {
		return out, fmt.Errorf("pipe consumer %s failed: %w (stderr: %s)",
			second.Name, consWaitErr, out.Stderr)
	}
	return out, nil
}

// buildEnv returns the parent process env plus the given overrides.
func buildEnv(overrides map[string]string) []string {
	if len(overrides) == 0 {
		return nil // means "inherit"
	}
	base := os.Environ()
	out := make([]string, 0, len(base)+len(overrides))
	skip := make(map[string]struct{}, len(overrides))
	for k := range overrides {
		skip[k] = struct{}{}
	}
	for _, kv := range base {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		if _, ok := skip[kv[:eq]]; ok {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

var _ Runner = Real{}
