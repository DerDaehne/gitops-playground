package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/DerDaehne/gitops-playground/internal/credentials"
)

type ScmManagerApiClient struct {
	client  *http.Client
	baseURL string
	creds   credentials.Credentials
}

type basicAuthTransport struct {
	username  string
	password  string
	transport http.RoundTripper
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(t.username, t.password)
	return t.transport.RoundTrip(req)
}

func NewScmManagerApiClient(baseURL string, creds credentials.Credentials) *ScmManagerApiClient {
	client := &http.Client{
		Transport: &basicAuthTransport{
			username:  creds.Username,
			password:  creds.Password,
			transport: http.DefaultTransport,
		},
	}
	return &ScmManagerApiClient{
		client:  client,
		baseURL: baseURL,
		creds:   creds,
	}
}

func (c *ScmManagerApiClient) doJSON(method string, path string, body any, contentType string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(jsonBytes)
	}

	url := c.baseURL + path
	slog.Debug("SCM-Manager API request", "method", method, "url", url)

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	return c.client.Do(req)
}

func handle201or409(resp *http.Response, what string) error {
	defer resp.Body.Close()
	code := resp.StatusCode
	if code == 409 {
		slog.Debug(fmt.Sprintf("%s already exists - ignoring (HTTP 409)", what))
		return nil
	}
	if code != 201 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("could not create %s. HTTP %d %s: %s", what, code, resp.Status, string(body))
	}
	return nil
}

func handleResponse(resp *http.Response, what string) error {
	defer resp.Body.Close()
	code := resp.StatusCode
	if code >= 200 && code < 300 || code == 409 || code == 201 {
		slog.Debug(fmt.Sprintf("Successfully completed API call: %s", what))
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("API call failed for %s. HTTP %d %s: %s", what, code, resp.Status, string(body))
}
