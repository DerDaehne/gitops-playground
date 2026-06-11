package jenkins

import (
	"context"
	"fmt"
	"strings"
)

// EnsureUser creates the Jenkins user if it does not already exist. Jenkins
// has no idempotent REST endpoint for accounts, so we drive it via the
// /scriptText Groovy bridge - same approach as UserManager.groovy. The
// mail and displayName fields are best-effort: Jenkins' UserDetails plugin
// rejects them silently when the security realm is read-only (e.g. CAS).
//
// The script first checks whether the login already exists and skips
// creation in that case, so EnsureUser is safe to call repeatedly.
func (c *Client) EnsureUser(ctx context.Context, login, password, displayName, mail string) error {
	if login == "" {
		return fmt.Errorf("jenkins: EnsureUser requires a login")
	}
	if err := assertNoBackslash("login", login); err != nil {
		return err
	}
	if err := assertNoBackslash("password", password); err != nil {
		return err
	}
	if err := assertNoBackslash("displayName", displayName); err != nil {
		return err
	}
	if err := assertNoBackslash("mail", mail); err != nil {
		return err
	}

	script := fmt.Sprintf(`
            def realm = Jenkins.getInstance().getSecurityRealm()
            def existing = hudson.model.User.getById('%s', false)
            def user
            if (existing != null) {
                user = existing
            } else {
                user = realm.createAccount('%s', '%s')
            }
            if ('%s' != '') {
                user.setFullName('%s')
            }
            try {
                def mailProp = user.getProperty(hudson.tasks.Mailer.UserProperty.class)
                if (mailProp == null && '%s' != '') {
                    user.addProperty(new hudson.tasks.Mailer.UserProperty('%s'))
                }
            } catch (Exception ignore) {}
            user.save()
            print(user.getId())
        `,
		escapeGroovy(login),
		escapeGroovy(login),
		escapeGroovy(password),
		escapeGroovy(displayName),
		escapeGroovy(displayName),
		escapeGroovy(mail),
		escapeGroovy(mail),
	)

	out, err := c.runScript(ctx, script)
	if err != nil {
		return fmt.Errorf("jenkins: EnsureUser %q: %w", login, err)
	}
	out = strings.TrimSpace(out)
	if out != login {
		return fmt.Errorf("jenkins: EnsureUser %q unexpected response: %q", login, out)
	}
	return nil
}

// GrantMetricsView grants the metrics-view permission to login when the
// active authorization strategy is matrix-based. Mirrors the
// UserManager.grantPermission(METRICS_VIEW) path used by ConfigurePrometheus.
func (c *Client) GrantMetricsView(ctx context.Context, login string) error {
	if err := assertNoBackslash("login", login); err != nil {
		return err
	}
	check := `print(Jenkins.getInstance().getAuthorizationStrategy().class)`
	out, err := c.runScript(ctx, check)
	if err != nil {
		return fmt.Errorf("jenkins: detect authorization strategy: %w", err)
	}
	out = strings.TrimSpace(out)
	if !strings.HasPrefix(out, "class ") {
		return fmt.Errorf("jenkins: unexpected authorization-strategy response: %q", out)
	}
	if out != "class hudson.security.GlobalMatrixAuthorizationStrategy" &&
		out != "class hudson.security.ProjectMatrixAuthorizationStrategy" {
		// Not matrix-based: nothing to do, same as Groovy UserManager.
		return nil
	}

	script := fmt.Sprintf(`
            import org.jenkinsci.plugins.matrixauth.PermissionEntry
            import org.jenkinsci.plugins.matrixauth.AuthorizationType

            def permissions = Jenkins.getInstance().getAuthorizationStrategy().getGrantedPermissionEntries()
            permissions.computeIfAbsent(jenkins.metrics.api.Metrics.VIEW) { new HashSet<>() }
            print(permissions[jenkins.metrics.api.Metrics.VIEW].add(new PermissionEntry(AuthorizationType.USER, '%s')))
        `, escapeGroovy(login))
	resp, err := c.runScript(ctx, script)
	if err != nil {
		return fmt.Errorf("jenkins: grant metrics view to %q: %w", login, err)
	}
	resp = strings.TrimSpace(resp)
	if resp != "true" && resp != "false" {
		return fmt.Errorf("jenkins: grant metrics view to %q returned %q", login, resp)
	}
	return nil
}

// IsUsingCASSecurityRealm reports whether Jenkins is configured to use the
// CAS plugin's security realm. CAS realms are read-only, so EnsureUser must
// be skipped in that case (see UserManager.isUsingCasSecurityRealm).
func (c *Client) IsUsingCASSecurityRealm(ctx context.Context) (bool, error) {
	out, err := c.runScript(ctx, `print(Jenkins.getInstance().getSecurityRealm().class)`)
	if err != nil {
		return false, err
	}
	out = strings.TrimSpace(out)
	if !strings.HasPrefix(out, "class ") {
		return false, fmt.Errorf("jenkins: unexpected security-realm response: %q", out)
	}
	return out == "class org.jenkinsci.plugins.cas.CasSecurityRealm", nil
}

// escapeGroovy mirrors UserManager.escapeString: single quotes are escaped,
// backslashes are forbidden (see assertNoBackslash).
func escapeGroovy(s string) string {
	return strings.ReplaceAll(s, "'", `\'`)
}

func assertNoBackslash(field, value string) error {
	if strings.Contains(value, `\`) {
		return fmt.Errorf("jenkins: %s must not contain backslashes", field)
	}
	return nil
}
