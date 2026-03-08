package api

import "fmt"

type ScmManagerUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Mail        string `json:"mail"`
	External    bool   `json:"external"`
	Password    string `json:"password"`
	Active      bool   `json:"active"`
}

func (c *ScmManagerApiClient) AddUser(user ScmManagerUser) error {
	resp, err := c.doJSON("POST", "v2/users", user, "application/vnd.scmm-user+json;v=2")
	if err != nil {
		return err
	}
	return handleResponse(resp, "add user "+user.Name)
}

func (c *ScmManagerApiClient) SetPermissionForUser(username string, permissions []string) error {
	body := map[string][]string{
		"permissions": permissions,
	}
	path := fmt.Sprintf("v2/users/%s/permissions", username)
	resp, err := c.doJSON("PUT", path, body, "application/vnd.scmm-permissionCollection+json;v=2")
	if err != nil {
		return err
	}
	return handleResponse(resp, "set permissions for user "+username)
}
