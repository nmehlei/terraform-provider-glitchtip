// src/glitchtip/client_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	return srv, srv.Close
}

func TestNew_RequiresEndpointAndToken(t *testing.T) {
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := New(context.Background(), Config{Endpoint: "https://example.com"}); err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestNew_RejectsPlainHTTPWithoutAllowInsecure(t *testing.T) {
	_, err := New(context.Background(), Config{Endpoint: "http://example.com", Token: "t"})
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *ConfigError, got %v (%T)", err, err)
	}
}

func TestNew_ChecksVersionAgainstOpenAPISchema(t *testing.T) {
	srv, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/openapi.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
	})
	defer closeFn()

	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "t", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected a client")
	}
}

func TestDo_DecodesJSONAndSetsAuthHeader(t *testing.T) {
	var gotAuth string
	srv, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]string{"slug": "acme"})
	})
	defer closeFn()

	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "secret-token", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out struct {
		Slug string `json:"slug"`
	}
	if err := c.do(context.Background(), http.MethodGet, "organizations/acme/", nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Slug != "acme" {
		t.Fatalf("expected slug acme, got %q", out.Slug)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("expected Bearer secret-token, got %q", gotAuth)
	}
}

func TestDo_ReturnsAPIErrorOnNon2xx(t *testing.T) {
	srv, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"not found"}`))
	})
	defer closeFn()

	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "t", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = c.do(context.Background(), http.MethodGet, "organizations/nope/", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v (%T)", err, err)
	}
	if !apiErr.NotFound() {
		t.Fatalf("expected NotFound() true, got status %d", apiErr.StatusCode)
	}
}

func TestDoPage_ParsesNextLinkHeader(t *testing.T) {
	srv, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		next := srv2URL(r) + `/api/0/organizations/?cursor=100:1:0`
		w.Header().Set("Link",
			`<`+next+`>; rel="next"; results="true"; cursor="100:1:0", `+
				`<`+next+`>; rel="previous"; results="false"; cursor="100:-1:1"`)
		_ = json.NewEncoder(w).Encode([]map[string]string{{"slug": "acme"}})
	})
	defer closeFn()

	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "t", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out []map[string]string
	next, err := c.doPage(context.Background(), http.MethodGet, "organizations/", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next == "" {
		t.Fatal("expected a non-empty next cursor URL")
	}
}

func TestDoPage_EmptyNextWhenResultsFalse(t *testing.T) {
	srv, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		w.Header().Set("Link", `<https://x/y?cursor=1:1:0>; rel="next"; results="false"; cursor="1:1:0"`)
		_ = json.NewEncoder(w).Encode([]map[string]string{})
	})
	defer closeFn()

	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "t", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out []map[string]string
	next, err := c.doPage(context.Background(), http.MethodGet, "organizations/", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next != "" {
		t.Fatalf("expected empty next when results=false, got %q", next)
	}
}

// srv2URL is a tiny helper so the Link-header fixture above can embed a
// syntactically valid absolute URL without depending on the server's own
// address at handler-definition time.
func srv2URL(r *http.Request) string {
	scheme := "http"
	return scheme + "://" + r.Host
}
