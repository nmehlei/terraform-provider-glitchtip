// src/glitchtip/client.go
// Package glitchtip is a minimal, hand-written HTTP client for a GlitchTip
// instance's REST API (/api/0/). It intentionally has no dependency on the
// Terraform Plugin Framework so it can be used and tested standalone.
package glitchtip

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	defaultTimeout = 30 * time.Second
	apiPrefix      = "api/0/"
	schemaPath     = "api/openapi.json"

	// maxResponseBody bounds how much of a response this client will ever
	// read into memory or embed in an error.
	maxResponseBody = 1 << 20 // 1 MiB
)

// Config configures a Client.
type Config struct {
	// Endpoint is the base URL of the GlitchTip instance, e.g.
	// "https://app.glitchtip.com". Must be HTTPS unless AllowInsecure is set.
	Endpoint string

	// Token authenticates every request as a bearer token.
	Token string

	// Timeout bounds every HTTP request. Defaults to 30s when zero.
	Timeout time.Duration

	// AllowInsecure permits a plain-HTTP endpoint. Intended only for local
	// development against a disposable instance.
	AllowInsecure bool

	// HTTPClient overrides the underlying HTTP client. Primarily for tests.
	HTTPClient *http.Client
}

// Client is a GlitchTip REST API client.
type Client struct {
	// baseURL is the resolved API root (endpoint + apiPrefix), with any
	// userinfo stripped so it can never leak into a request URL or error.
	baseURL    *url.URL
	rootURL    *url.URL // endpoint root, for schemaPath
	token      string
	timeout    time.Duration
	httpClient *http.Client
}

// New validates cfg, fetches and checks the remote OpenAPI schema is
// reachable, and returns a ready Client.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, &ConfigError{Msg: "endpoint is required"}
	}
	if cfg.Token == "" {
		return nil, &ConfigError{Msg: "token is required"}
	}

	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, &ConfigError{Msg: "invalid endpoint", Err: err}
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !cfg.AllowInsecure {
			return nil, &ConfigError{Msg: "endpoint uses plain HTTP; set AllowInsecure for local development only"}
		}
	default:
		return nil, &ConfigError{Msg: fmt.Sprintf("unsupported endpoint scheme %q", u.Scheme)}
	}

	root := *u
	root.User = nil // never let a credential embedded in the endpoint leak
	root.RawQuery = ""
	root.Fragment = ""
	if !strings.HasSuffix(root.Path, "/") {
		root.Path += "/"
	}

	base := root
	base.Path += apiPrefix

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	c := &Client{
		baseURL:    &base,
		rootURL:    &root,
		token:      cfg.Token,
		timeout:    timeout,
		httpClient: httpClient,
	}

	if err := c.checkVersion(ctx); err != nil {
		return nil, err
	}

	return c, nil
}

type openAPISchema struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
}

func (c *Client) checkVersion(ctx context.Context) error {
	reqURL := c.rootURL.ResolveReference(&url.URL{Path: schemaPath}).String()
	// Reuses do's transport/auth/timeout plumbing by resolving relative
	// to rootURL directly rather than baseURL — schemaPath sits outside
	// api/0/.
	var schema openAPISchema
	if err := c.doURL(ctx, http.MethodGet, reqURL, nil, &schema); err != nil {
		return &ConfigError{Msg: "fetching OpenAPI schema", Err: err}
	}
	if schema.Info.Version == "" {
		return &ConfigError{Msg: "OpenAPI schema at " + schemaPath + " had no info.version; is this a GlitchTip instance?"}
	}
	return nil
}

// do issues a request against a path relative to the API root and decodes a
// JSON response body into out when out is non-nil.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	reqURL := c.resolve(path)
	_, err := c.request(ctx, method, reqURL, body, out)
	return err
}

// doURL is like do, but reqURL is already absolute (used by checkVersion,
// which requests schemaPath outside the api/0/ prefix do resolves under).
func (c *Client) doURL(ctx context.Context, method, reqURL string, body io.Reader, out any) error {
	_, err := c.request(ctx, method, reqURL, body, out)
	return err
}

// doPage is like do, but for paginated list endpoints: it also returns the
// "next" cursor URL parsed from the response's Link header, or "" when
// there is no further page. path may be a path relative to the API root
// (first page) or an absolute cursor URL previously returned by doPage
// itself (a later page) — resolve leaves absolute URLs untouched.
func (c *Client) doPage(ctx context.Context, method, path string, out any) (string, error) {
	reqURL := c.resolve(path)
	link, err := c.request(ctx, method, reqURL, nil, out)
	if err != nil {
		return "", err
	}
	return parseNextLink(link), nil
}

// resolve turns path into an absolute request URL. A relative path is
// resolved against the API root; an absolute URL (as returned by doPage
// for pagination) is returned unchanged — checkSameHost still rejects it
// in request if it points off-host.
func (c *Client) resolve(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return c.baseURL.ResolveReference(&url.URL{Path: c.baseURL.Path + strings.TrimPrefix(path, "/")}).String()
}

var linkPartRe = regexp.MustCompile(`<([^>]+)>((?:\s*;\s*[a-zA-Z]+="[^"]*")*)`)

// parseNextLink extracts the rel="next" URL from a Sentry/GlitchTip-style
// cursor Link header, returning "" if there is no next page (results="false"
// or no rel="next" segment at all).
func parseNextLink(header string) string {
	if header == "" {
		return ""
	}
	for _, m := range linkPartRe.FindAllStringSubmatch(header, -1) {
		url, params := m[1], m[2]
		if !strings.Contains(params, `rel="next"`) {
			continue
		}
		if strings.Contains(params, `results="false"`) {
			return ""
		}
		return url
	}
	return ""
}

// request is the single low-level entry point every other method funnels
// through. It returns the response's Link header so doPage can parse it;
// do/doURL ignore that return value.
func (c *Client) request(ctx context.Context, method, reqURL string, body io.Reader, out any) (string, error) {
	if err := c.checkSameHost(reqURL); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "terraform-provider-glitchtip-client")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("glitchtip: %s %s: %w", method, reqURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return "", fmt.Errorf("glitchtip: %s %s: reading response: %w", method, reqURL, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &APIError{Method: method, URL: reqURL, StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return "", fmt.Errorf("glitchtip: %s %s: decoding response (status %d, content-type %q): %w",
				method, reqURL, resp.StatusCode, resp.Header.Get("Content-Type"), err)
		}
	}

	return resp.Header.Get("Link"), nil
}

// checkSameHost refuses to follow an absolute pagination URL to a
// different host than the configured endpoint, so a malicious or
// misconfigured server can't redirect the client's bearer token elsewhere.
func (c *Client) checkSameHost(reqURL string) error {
	u, err := url.Parse(reqURL)
	if err != nil {
		return fmt.Errorf("glitchtip: invalid request URL %q: %w", reqURL, err)
	}
	if u.User != nil {
		return &ConfigError{Msg: "refusing to follow a URL that embeds credentials"}
	}
	if !strings.EqualFold(u.Scheme, c.baseURL.Scheme) || !strings.EqualFold(u.Host, c.baseURL.Host) {
		return &ConfigError{Msg: fmt.Sprintf(
			"refusing to follow a URL to a different host (%s://%s), configured endpoint is %s://%s",
			u.Scheme, u.Host, c.baseURL.Scheme, c.baseURL.Host,
		)}
	}
	return nil
}
