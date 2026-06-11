package jenkins

import (
	"context"
	"fmt"
	"net/http"
)

// DeleteJob removes a job (or folder) from Jenkins. The fullName can
// contain slashes for nested folders. Mirrors JobManager.deleteJob in the
// Groovy code. An already-absent job is treated as success.
func (c *Client) DeleteJob(ctx context.Context, fullName string) error {
	if fullName == "" {
		return fmt.Errorf("jenkins: DeleteJob requires a job name")
	}
	endpoint := jobURLPath(fullName) + "/doDelete"
	resp, _, err := c.doRequest(ctx, http.MethodPost, endpoint, nil, "", true)
	if err != nil {
		return fmt.Errorf("jenkins: DeleteJob %q: %w", fullName, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	// Jenkins answers 200/302 on success, 404 when the job is already gone.
	case http.StatusOK, http.StatusFound, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("jenkins: DeleteJob %q returned HTTP %d", fullName, resp.StatusCode)
	}
}
