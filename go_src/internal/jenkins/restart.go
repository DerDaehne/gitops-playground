package jenkins

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// RestartSafely triggers /safeRestart and waits for Jenkins to come back
// online. Bug-fix vs. the Groovy implementation: we poll a real health
// endpoint until it returns 200 instead of an ad-hoc Thread.sleep. The poll
// interval respects the supplied ctx; cancellation aborts the wait.
//
// Health detection: we first probe /login (always reachable, no auth
// required) and fall back to /api/json. As soon as one of them returns 2xx
// or 403 (Jenkins is up but rejecting our request - that still means the
// process is healthy), RestartSafely returns nil.
func (c *Client) RestartSafely(ctx context.Context) error {
	// triggerRestart errors are not fatal: Jenkins may tear down its HTTP
	// listener while the request is still in flight. We still wait for the
	// health endpoint to come back below. Only a cancelled context aborts
	// the call here.
	_ = c.triggerRestart(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	interval := c.RetryDelay
	if interval <= 0 {
		interval = 2 * time.Second
	}

	deadline, hasDeadline := ctx.Deadline()
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if c.pingHealth(ctx) {
			// Crumb is no longer valid after a restart, so clear the cache.
			c.invalidateCrumb()
			return nil
		}

		if hasDeadline && time.Until(deadline) <= interval {
			// Last attempt before the deadline expires.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Until(deadline)):
				if c.pingHealth(ctx) {
					c.invalidateCrumb()
					return nil
				}
				return fmt.Errorf("jenkins: timed out waiting for restart")
			}
		}

		if !sleepWithCtx(ctx, interval) {
			return ctx.Err()
		}
	}
}

func (c *Client) triggerRestart(ctx context.Context) error {
	resp, body, err := c.doRequest(ctx, http.MethodPost, "safeRestart", nil, "", true)
	if err != nil {
		// During a restart Jenkins may already start tearing down before
		// returning a response. The polling loop below treats that case
		// uniformly, so we propagate the trigger error only if it's a
		// context cancellation.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("jenkins: trigger safeRestart: %w", err)
	}
	// safeRestart returns 200 on the form-style endpoint and 302 when
	// redirecting back to the management page.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		return fmt.Errorf("jenkins: safeRestart returned %d: %s", resp.StatusCode, truncate(body, 200))
	}
	return nil
}

// pingHealth performs a short non-mutating probe against Jenkins. Returns
// true as soon as the process answers with anything that is not a connection
// error or 5xx response.
func (c *Client) pingHealth(ctx context.Context) bool {
	for _, path := range []string{"login", "api/json"} {
		u, err := c.resolve(path)
		if err != nil {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		c.applyAuth(req)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			continue
		}
		drain(resp)
		// Anything that's not a 5xx or connection error means Jenkins is
		// reachable again. 401/403 still means the process is up.
		if resp.StatusCode < 500 {
			return true
		}
	}
	return false
}
