package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client is the OpenEdge REST API client.
type Client struct {
	baseURL    string
	token      string
	orgID      int
	httpClient *http.Client
}

// New creates a new API client.
func New(baseURL, token string, orgID int) *Client {
	return NewWithOptions(baseURL, token, orgID, false)
}

// NewWithOptions creates a new API client with extra options.
// insecure=true skips TLS certificate verification (useful for on-prem self-signed certs).
func NewWithOptions(baseURL, token string, orgID int, insecure bool) *Client {
	transport := http.DefaultTransport
	if insecure {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		orgID:   orgID,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

// Request executes an HTTP request, setting auth headers.
// body may be nil. If result is non-nil the response JSON is decoded into it.
func (c *Client) Request(method, path string, body, result interface{}) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	url := c.baseURL + path
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if c.orgID > 0 {
		req.Header.Set("X-Organization-ID", strconv.Itoa(c.orgID))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to extract error message from JSON
		var errBody map[string]interface{}
		if json.Unmarshal(respData, &errBody) == nil {
			if msg, ok := errBody["error"].(string); ok {
				return fmt.Errorf("API error %d: %s", resp.StatusCode, msg)
			}
		}
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respData))
	}

	if result != nil && len(respData) > 0 {
		if err := json.Unmarshal(respData, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// Get executes a GET request.
func (c *Client) Get(path string, result interface{}) error {
	return c.Request("GET", path, nil, result)
}

// Post executes a POST request.
func (c *Client) Post(path string, body, result interface{}) error {
	return c.Request("POST", path, body, result)
}

// Put executes a PUT request.
func (c *Client) Put(path string, body, result interface{}) error {
	return c.Request("PUT", path, body, result)
}

// RawGet executes a GET and returns the raw bytes.
func (c *Client) RawGet(path string) ([]byte, error) {
	url := c.baseURL + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.orgID > 0 {
		req.Header.Set("X-Organization-ID", strconv.Itoa(c.orgID))
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// RawPost executes a POST and returns the raw bytes.
func (c *Client) RawPost(path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}
	url := c.baseURL + path
	req, err := http.NewRequest("POST", url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if c.orgID > 0 {
		req.Header.Set("X-Organization-ID", strconv.Itoa(c.orgID))
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// LoginRequest is the payload for POST /api/auth/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is returned by POST /api/auth/login.
type LoginResponse struct {
	Token            string `json:"token"`
	MFARequired      bool   `json:"mfa_required"`
	MFAToken         string `json:"mfa_token"`
	MFASetupRequired bool   `json:"mfa_setup_required"`
	User             struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
		OrgID    *int   `json:"org_id"`
	} `json:"user"`
}

// Login authenticates and returns a JWT token.
func Login(baseURL, username, password string) (LoginResponse, error) {
	return LoginWithOptions(baseURL, username, password, false)
}

// LoginWithOptions authenticates with optional TLS skip (for on-prem self-signed certs).
func LoginWithOptions(baseURL, username, password string, insecure bool) (LoginResponse, error) {
	transport := http.DefaultTransport
	if insecure {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}
	httpClient := &http.Client{Timeout: 15 * time.Second, Transport: transport}

	payload, _ := json.Marshal(LoginRequest{Username: username, Password: password})
	url := strings.TrimRight(baseURL, "/") + "/api/auth/login"

	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return LoginResponse{}, fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return LoginResponse{}, fmt.Errorf("read login response: %w", err)
	}

	if resp.StatusCode != 200 {
		var errBody map[string]interface{}
		if json.Unmarshal(data, &errBody) == nil {
			if msg, ok := errBody["error"].(string); ok {
				return LoginResponse{}, fmt.Errorf("login failed: %s", msg)
			}
		}
		return LoginResponse{}, fmt.Errorf("login failed (HTTP %d): %s", resp.StatusCode, string(data))
	}

	var loginResp LoginResponse
	if err := json.Unmarshal(data, &loginResp); err != nil {
		return LoginResponse{}, fmt.Errorf("decode login response: %w", err)
	}

	return loginResp, nil
}

// VerifyMFA exchanges a mfa_token + TOTP/recovery code for a full JWT.
func VerifyMFA(baseURL, mfaToken, code string, insecure bool) (LoginResponse, error) {
	transport := http.DefaultTransport
	if insecure {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}
	httpClient := &http.Client{Timeout: 15 * time.Second, Transport: transport}

	payload, _ := json.Marshal(map[string]string{"mfa_token": mfaToken, "code": code})
	apiURL := strings.TrimRight(baseURL, "/") + "/api/auth/mfa/verify"

	resp, err := httpClient.Post(apiURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return LoginResponse{}, fmt.Errorf("MFA verify request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return LoginResponse{}, fmt.Errorf("read MFA response: %w", err)
	}

	if resp.StatusCode != 200 {
		var errBody map[string]interface{}
		if json.Unmarshal(data, &errBody) == nil {
			if msg, ok := errBody["error"].(string); ok {
				return LoginResponse{}, fmt.Errorf("MFA failed: %s", msg)
			}
		}
		return LoginResponse{}, fmt.Errorf("MFA failed (HTTP %d)", resp.StatusCode)
	}

	var r LoginResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return LoginResponse{}, fmt.Errorf("decode MFA response: %w", err)
	}
	return r, nil
}

// RawDelete executes a DELETE and returns the raw bytes.
//
// Present because the MCP server can now remove things — a tag, a UDT
// instance, a mimic page — and an agent that can create without being able to
// delete leaves a plant model that only ever grows.
func (c *Client) RawDelete(path string) ([]byte, error) {
	req, err := http.NewRequest("DELETE", c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.orgID > 0 {
		req.Header.Set("X-Organization-ID", strconv.Itoa(c.orgID))
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}
