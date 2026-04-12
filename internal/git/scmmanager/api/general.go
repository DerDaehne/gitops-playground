package api

import "fmt"

func (c *ScmManagerApiClient) CheckAvailable() error {
	resp, err := c.doJSON("GET", "v2", nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("SCM-Manager not available: HTTP %d", resp.StatusCode)
}

func (c *ScmManagerApiClient) SetConfig(config map[string]any) error {
	resp, err := c.doJSON("PUT", "v2/config", config, "application/vnd.scmm-config+json;v=2")
	if err != nil {
		return err
	}
	return handleResponse(resp, "set SCM-Manager config")
}
