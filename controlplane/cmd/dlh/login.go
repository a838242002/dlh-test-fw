package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func loginCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "login",
		Short: "Authenticate via OIDC device-code flow + cache the token at ~/.config/dlh/token",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runLogin()
		},
	}
	return c
}

func runLogin() error {
	// Step 1: consult the controlplane for IdP config.
	infoURL := strings.TrimRight(flagEndpoint, "/") + "/api/auth/info"
	resp, err := http.Get(infoURL)
	if err != nil {
		return fmt.Errorf("query auth info: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("auth info HTTP %d: %s", resp.StatusCode, body)
	}
	var info struct {
		OidcIssuer   string `json:"oidcIssuer"`
		OidcClientId string `json:"oidcClientId"`
		AuthDisabled bool   `json:"authDisabled"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("parse auth info: %w", err)
	}
	if info.AuthDisabled {
		return fmt.Errorf("controlplane has DLH_AUTH_DISABLED=true; no login needed")
	}
	if info.OidcIssuer == "" || info.OidcClientId == "" {
		return fmt.Errorf("controlplane auth info missing issuer or client id")
	}

	// Step 2: discover the device authorization endpoint.
	disco, err := discoverOIDC(info.OidcIssuer)
	if err != nil {
		return fmt.Errorf("oidc discovery: %w", err)
	}
	if disco.DeviceAuthorizationEndpoint == "" || disco.TokenEndpoint == "" {
		return fmt.Errorf("issuer does not expose device-code endpoints")
	}

	// Step 3: initiate device code grant.
	dc, err := requestDeviceCode(disco.DeviceAuthorizationEndpoint, info.OidcClientId)
	if err != nil {
		return fmt.Errorf("device code: %w", err)
	}
	fmt.Printf("\nVisit: %s\nEnter code: %s\n\n", dc.VerificationURI, dc.UserCode)
	if dc.VerificationURIComplete != "" {
		fmt.Printf("(Or open: %s)\n\n", dc.VerificationURIComplete)
	}

	// Step 4: poll for the id_token.
	idToken, err := pollForToken(disco.TokenEndpoint, info.OidcClientId, dc)
	if err != nil {
		return fmt.Errorf("poll: %w", err)
	}

	// Step 5: persist to ~/.config/dlh/token.
	if err := saveToken(idToken); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	fmt.Println("Login successful. Token cached at ~/.config/dlh/token.")
	return nil
}

type oidcDiscovery struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

func discoverOIDC(issuer string) (*oidcDiscovery, error) {
	u := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discovery HTTP %d: %s", resp.StatusCode, body)
	}
	var d oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

type deviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func requestDeviceCode(endpoint, clientID string) (*deviceCode, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", "openid profile email")
	resp, err := http.PostForm(endpoint, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("device code HTTP %d: %s", resp.StatusCode, body)
	}
	var d deviceCode
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	if d.Interval <= 0 {
		d.Interval = 5
	}
	if d.ExpiresIn <= 0 {
		d.ExpiresIn = 600
	}
	return &d, nil
}

func pollForToken(tokenEndpoint, clientID string, dc *deviceCode) (string, error) {
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	interval := time.Duration(dc.Interval) * time.Second
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("device code expired")
		case <-t.C:
			form := url.Values{}
			form.Set("client_id", clientID)
			form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
			form.Set("device_code", dc.DeviceCode)
			resp, err := http.PostForm(tokenEndpoint, form)
			if err != nil {
				return "", err
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var tr struct {
				IdToken string `json:"id_token"`
				Error   string `json:"error"`
			}
			_ = json.Unmarshal(body, &tr)
			if tr.IdToken != "" {
				return tr.IdToken, nil
			}
			switch tr.Error {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5 * time.Second
				t.Reset(interval)
			default:
				return "", fmt.Errorf("token endpoint: %s — %s", tr.Error, body)
			}
		}
	}
}

func saveToken(idToken string) error {
	dir, err := tokenDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "token"), []byte(idToken), 0o600)
}

func tokenDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "dlh"), nil
}

func loadCachedToken() string {
	dir, err := tokenDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
