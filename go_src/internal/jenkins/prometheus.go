package jenkins

import (
	"context"
	"fmt"
	"strings"
)

// PrometheusConfig bundles the metrics-endpoint setup that
// PrometheusConfigurator + UserManager performed together in the Groovy
// implementation. MetricsUsername/Password are optional: when empty, the
// caller has already created the user and we only flip the authentication
// switch.
type PrometheusConfig struct {
	MetricsUsername    string
	MetricsPassword    string
	MetricsDisplayName string
	MetricsMail        string
	SkipUserCreation   bool
}

// ConfigurePrometheus enables Jenkins' authenticated Prometheus endpoint
// and (optionally) creates the metrics user + grants it the metrics-view
// permission. This combines the two responsibilities that the Groovy code
// split across PrometheusConfigurator.enableAuthentication and
// UserManager.createUser/grantPermission.
func (c *Client) ConfigurePrometheus(ctx context.Context, p PrometheusConfig) error {
	if !p.SkipUserCreation && p.MetricsUsername != "" {
		cas, err := c.IsUsingCASSecurityRealm(ctx)
		if err != nil {
			return fmt.Errorf("jenkins: ConfigurePrometheus: %w", err)
		}
		if !cas {
			if err := c.EnsureUser(ctx, p.MetricsUsername, p.MetricsPassword, p.MetricsDisplayName, p.MetricsMail); err != nil {
				return fmt.Errorf("jenkins: ConfigurePrometheus: %w", err)
			}
		}
	}

	if p.MetricsUsername != "" {
		if err := c.GrantMetricsView(ctx, p.MetricsUsername); err != nil {
			return fmt.Errorf("jenkins: ConfigurePrometheus: %w", err)
		}
	}

	out, err := c.runScript(ctx, `
            import org.jenkinsci.plugins.prometheus.config.*

            def config = Jenkins.instance.getDescriptor(PrometheusConfiguration)
            config.setUseAuthenticatedEndpoint(true)

            print(config.useAuthenticatedEndpoint)
        `)
	if err != nil {
		return fmt.Errorf("jenkins: enable prometheus authentication: %w", err)
	}
	if strings.TrimSpace(out) != "true" {
		return fmt.Errorf("jenkins: enable prometheus authentication returned %q", out)
	}
	return nil
}
