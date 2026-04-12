package api

import "fmt"

func (c *ScmManagerApiClient) InstallPlugin(name string, restart bool) error {
	restartStr := "false"
	if restart {
		restartStr = "true"
	}
	path := fmt.Sprintf("v2/plugins/available/%s/install?restart=%s", name, restartStr)
	resp, err := c.doJSON("POST", path, nil, "")
	if err != nil {
		return err
	}
	return handleResponse(resp, "install plugin "+name)
}

func (c *ScmManagerApiClient) ConfigureJenkinsPlugin(config map[string]any) error {
	resp, err := c.doJSON("PUT", "v2/config/jenkins/", config, "application/json")
	if err != nil {
		return err
	}
	return handleResponse(resp, "configure Jenkins plugin")
}
