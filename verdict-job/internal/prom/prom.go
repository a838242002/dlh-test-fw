// Package prom is a tiny PromQL client. Vector-instant queries only — that's all verdict needs.
package prom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type API interface {
	QueryAt(ctx context.Context, q string, t time.Time) (float64, error)
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type queryResp struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Value [2]any `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func (c *Client) QueryAt(ctx context.Context, q string, t time.Time) (float64, error) {
	u := c.BaseURL + "/api/v1/query?query=" + url.QueryEscape(q) +
		"&time=" + strconv.FormatInt(t.Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("prom: HTTP %d: %s", resp.StatusCode, body)
	}
	var qr queryResp
	if err := json.Unmarshal(body, &qr); err != nil {
		return 0, fmt.Errorf("prom: decode: %w", err)
	}
	if qr.Status != "success" {
		return 0, fmt.Errorf("prom: status=%s body=%s", qr.Status, body)
	}
	if len(qr.Data.Result) == 0 {
		return 0, nil // empty result → 0 (caller decides if that's pass or fail)
	}
	sv, ok := qr.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("prom: value field not a string: %v", qr.Data.Result[0].Value[1])
	}
	return strconv.ParseFloat(sv, 64)
}
