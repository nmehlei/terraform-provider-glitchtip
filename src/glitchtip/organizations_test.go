// src/glitchtip/organizations_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newOpenAPIAwareMux(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		handle(w, r)
	}))
}

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "t", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return c
}

func TestCreateGetUpdateDeleteOrganization(t *testing.T) {
	orgs := map[string]Organization{}
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/organizations/":
			var in CreateOrganizationRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			org := Organization{ID: "1", Slug: "acme", Name: in.Name}
			orgs["acme"] = org
			_ = json.NewEncoder(w).Encode(org)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/organizations/acme/":
			org, ok := orgs["acme"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(org)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/organizations/acme/":
			var in UpdateOrganizationRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			org := orgs["acme"]
			if in.Name != nil {
				org.Name = *in.Name
			}
			orgs["acme"] = org
			_ = json.NewEncoder(w).Encode(org)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/organizations/acme/":
			delete(orgs, "acme")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	created, err := c.CreateOrganization(ctx, CreateOrganizationRequest{Name: "Acme"})
	if err != nil || created.Slug != "acme" {
		t.Fatalf("create: %+v, err=%v", created, err)
	}

	got, err := c.GetOrganization(ctx, "acme")
	if err != nil || got.Name != "Acme" {
		t.Fatalf("get: %+v, err=%v", got, err)
	}

	newName := "Acme Corp"
	updated, err := c.UpdateOrganization(ctx, "acme", UpdateOrganizationRequest{Name: &newName})
	if err != nil || updated.Name != "Acme Corp" {
		t.Fatalf("update: %+v, err=%v", updated, err)
	}

	if err := c.DeleteOrganization(ctx, "acme"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = c.GetOrganization(ctx, "acme")
	var apiErr *APIError
	if err == nil {
		t.Fatal("expected error after delete")
	}
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}

func TestListOrganizations_Paginates(t *testing.T) {
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			w.Header().Set("Link", `<`+"http://"+r.Host+`/api/0/organizations/?cursor=1>; rel="next"; results="true"; cursor="1"`)
			_ = json.NewEncoder(w).Encode([]Organization{{ID: "1", Slug: "acme", Name: "Acme"}})
			return
		}
		w.Header().Set("Link", `<x>; rel="next"; results="false"; cursor="2"`)
		_ = json.NewEncoder(w).Encode([]Organization{{ID: "2", Slug: "beta", Name: "Beta"}})
	})
	defer srv.Close()
	c := newTestClient(t, srv)

	page1, next1, err := c.ListOrganizations(context.Background(), "")
	if err != nil || len(page1) != 1 || next1 == "" {
		t.Fatalf("page1: %+v next=%q err=%v", page1, next1, err)
	}

	page2, next2, err := c.ListOrganizations(context.Background(), next1)
	if err != nil || len(page2) != 1 || next2 != "" {
		t.Fatalf("page2: %+v next=%q err=%v", page2, next2, err)
	}
}
