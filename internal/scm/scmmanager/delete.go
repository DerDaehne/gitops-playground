package scmmanager

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DeleteRepository removes a repository from the SCM-Manager. Mirrors
// ScmmDestructionHandler.deleteRepository in the Groovy source.
//
// SCM-Manager returns 204 for a successful delete and 404 when the repo
// is already gone; both are treated as success so destroy runs are
// idempotent.
func (c *Client) DeleteRepository(ctx context.Context, namespace, name string) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("scm-manager: DeleteRepository requires namespace and name")
	}
	path := fmt.Sprintf("v2/repositories/%s/%s", urlEscape(namespace), urlEscape(name))
	resp, err := c.do(ctx, http.MethodDelete, path, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK, http.StatusNotFound:
		return nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		return fmt.Errorf("scm-manager: delete repo %s/%s returned HTTP %d %s: %s",
			namespace, name, resp.StatusCode, http.StatusText(resp.StatusCode),
			strings.TrimSpace(string(body)))
	}
}

// DeleteUser removes a user from the SCM-Manager. Mirrors
// ScmmDestructionHandler.deleteUser. Like DeleteRepository, an already
// absent user is treated as success.
func (c *Client) DeleteUser(ctx context.Context, login string) error {
	if login == "" {
		return fmt.Errorf("scm-manager: DeleteUser requires a login")
	}
	path := fmt.Sprintf("v2/users/%s", urlEscape(login))
	resp, err := c.do(ctx, http.MethodDelete, path, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK, http.StatusNotFound:
		return nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		return fmt.Errorf("scm-manager: delete user %s returned HTTP %d %s: %s",
			login, resp.StatusCode, http.StatusText(resp.StatusCode),
			strings.TrimSpace(string(body)))
	}
}

// urlEscape is a tiny path-segment escaper. The SCM-Manager paths only
// allow [A-Za-z0-9._-] in segments, so a single pass replacing the rare
// special character is enough; we keep the function private to avoid
// pulling net/url into hot paths.
func urlEscape(seg string) string {
	if !strings.ContainsAny(seg, "/?#%") {
		return seg
	}
	r := strings.NewReplacer(
		"%", "%25",
		"/", "%2F",
		"?", "%3F",
		"#", "%23",
	)
	return r.Replace(seg)
}
