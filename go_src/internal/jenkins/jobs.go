package jenkins

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// minimal Jenkins folder config.xml used by ensureFolder. This carries
// no specific configuration on purpose: callers can layer credentials or
// children on top.
const folderConfigXML = `<?xml version='1.1' encoding='UTF-8'?>
<com.cloudbees.hudson.plugins.folder.Folder>
  <description></description>
  <properties/>
  <folderViews class="com.cloudbees.hudson.plugins.folder.views.DefaultFolderViewHolder">
    <views>
      <hudson.model.AllView>
        <name>All</name>
      </hudson.model.AllView>
    </views>
  </folderViews>
  <healthMetrics/>
</com.cloudbees.hudson.plugins.folder.Folder>`

// CreateOrUpdateJob creates a job called name inside folder (may be empty
// for the Jenkins root) using the supplied config.xml payload. If the job
// already exists, its config is updated via POST /job/.../config.xml. If the
// folder does not exist yet, it is created using the standard Jenkins
// Folder XML.
//
// folder may itself be nested (e.g. "team-a/sub"); each segment is created
// if missing.
func (c *Client) CreateOrUpdateJob(ctx context.Context, folder, name, configXML string) error {
	if name == "" {
		return fmt.Errorf("jenkins: job name is required")
	}
	if folder != "" {
		if err := c.ensureFolderPath(ctx, folder); err != nil {
			return err
		}
	}

	exists, err := c.jobExists(ctx, folder, name)
	if err != nil {
		return err
	}
	if exists {
		return c.updateJobConfig(ctx, folder, name, configXML)
	}
	return c.createItem(ctx, folder, name, configXML)
}

// RunJob triggers a build for fullName. fullName is the slash-separated
// path inside Jenkins (e.g. "folder/sub/job"). Optional build parameters
// map onto the buildWithParameters form-style endpoint.
func (c *Client) RunJob(ctx context.Context, fullName string, params map[string]string) error {
	if fullName == "" {
		return fmt.Errorf("jenkins: job fullName is required")
	}
	rel := jobURLPath(fullName)
	endpoint := rel + "/build?delay=0sec"
	var body string
	contentType := ""
	if len(params) > 0 {
		endpoint = rel + "/buildWithParameters"
		form := url.Values{}
		for k, v := range params {
			form.Set(k, v)
		}
		body = form.Encode()
		contentType = "application/x-www-form-urlencoded"
	}

	var reader = strings.NewReader(body)
	resp, respBody, err := c.doRequest(ctx, http.MethodPost, endpoint, reader, contentType, true)
	if err != nil {
		return fmt.Errorf("jenkins: trigger job %q: %w", fullName, err)
	}
	// Jenkins returns 201 with a Location header for queued builds; 200 is
	// also accepted (older plugin versions).
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jenkins: trigger job %q returned %d: %s", fullName, resp.StatusCode, truncate(respBody, 200))
	}
	return nil
}

// ensureFolderPath walks folder segments left-to-right and creates each one
// when missing. This is the "folder-create-if-missing" pattern alluded to
// in the spec; the Groovy JobManager never did this explicitly.
func (c *Client) ensureFolderPath(ctx context.Context, folder string) error {
	parts := splitFolderPath(folder)
	parent := ""
	for _, p := range parts {
		if err := c.ensureSingleFolder(ctx, parent, p); err != nil {
			return err
		}
		if parent == "" {
			parent = p
		} else {
			parent = parent + "/" + p
		}
	}
	return nil
}

func (c *Client) ensureSingleFolder(ctx context.Context, parent, name string) error {
	exists, err := c.jobExists(ctx, parent, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return c.createItem(ctx, parent, name, folderConfigXML)
}

// jobExists returns true when GET /<path>/api/json returns 200.
func (c *Client) jobExists(ctx context.Context, folder, name string) (bool, error) {
	rel := jobURLPath(joinFolder(folder, name)) + "/api/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mustResolve(c, rel), nil)
	if err != nil {
		return false, err
	}
	c.applyAuth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, fmt.Errorf("jenkins: check job %q: %w", name, err)
	}
	defer drain(resp)
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("jenkins: check job %q returned %d", name, resp.StatusCode)
	}
}

func (c *Client) createItem(ctx context.Context, folder, name, configXML string) error {
	endpoint := "createItem?name=" + url.QueryEscape(name)
	if folder != "" {
		endpoint = jobURLPath(folder) + "/createItem?name=" + url.QueryEscape(name)
	}
	resp, body, err := c.doRequest(ctx, http.MethodPost, endpoint, strings.NewReader(configXML), "application/xml", true)
	if err != nil {
		return fmt.Errorf("jenkins: create item %q: %w", name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jenkins: create item %q returned %d: %s", name, resp.StatusCode, truncate(body, 200))
	}
	return nil
}

func (c *Client) updateJobConfig(ctx context.Context, folder, name, configXML string) error {
	endpoint := jobURLPath(joinFolder(folder, name)) + "/config.xml"
	resp, body, err := c.doRequest(ctx, http.MethodPost, endpoint, strings.NewReader(configXML), "application/xml", true)
	if err != nil {
		return fmt.Errorf("jenkins: update job %q: %w", name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jenkins: update job %q returned %d: %s", name, resp.StatusCode, truncate(body, 200))
	}
	return nil
}

// jobURLPath converts a logical job path ("folder/sub/job") into the
// Jenkins REST path ("job/folder/job/sub/job/job").
func jobURLPath(full string) string {
	parts := splitFolderPath(full)
	if len(parts) == 0 {
		return ""
	}
	segs := make([]string, 0, len(parts)*2)
	for _, p := range parts {
		segs = append(segs, "job", url.PathEscape(p))
	}
	return strings.Join(segs, "/")
}

func splitFolderPath(s string) []string {
	out := make([]string, 0, 4)
	for _, p := range strings.Split(s, "/") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func joinFolder(folder, name string) string {
	if folder == "" {
		return name
	}
	return strings.TrimRight(folder, "/") + "/" + name
}

// mustResolve is a small helper used in places where the URL is built from
// known-good components; it returns an empty string on error so the caller
// gets a nicely typed *http.NewRequestWithContext error instead.
func mustResolve(c *Client, rel string) string {
	u, err := c.resolve(rel)
	if err != nil {
		return ""
	}
	return u
}
