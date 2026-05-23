package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type apiClient struct {
	endpoint string
	token    string
	http     *http.Client
}

func newClient() *apiClient {
	return &apiClient{
		endpoint: strings.TrimRight(flagEndpoint, "/"),
		token:    flagToken,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *apiClient) do(method, path string, body interface{}, query url.Values) ([]byte, int, error) {
	if c.token == "" {
		return nil, 0, fmt.Errorf("no bearer token configured; set DLH_TOKEN or run 'dlh login'\n(local dev: export DLH_TOKEN=\"fake:admin:admin@local:dlh-admin\")")
	}
	full := c.endpoint + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, full, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return respBody, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, resp.StatusCode, nil
}

// newRequestWithAuth builds a Request with Authorization set, used by the
// SSE streaming path which doesn't fit do()'s body-decode flow.
func newRequestWithAuth(method, fullURL string) (*http.Request, error) {
	if flagToken == "" {
		return nil, fmt.Errorf("no bearer token configured; set DLH_TOKEN or run 'dlh login'\n(local dev: export DLH_TOKEN=\"fake:admin:admin@local:dlh-admin\")")
	}
	req, err := http.NewRequest(method, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+flagToken)
	return req, nil
}
