// src/glitchtip/projects_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateGetUpdateDeleteProject(t *testing.T) {
	projects := map[string]Project{}
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/teams/acme/platform/projects/":
			var in CreateProjectRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			p := Project{ID: "100", Slug: "checkout", Name: in.Name, Platform: in.Platform,
				Teams: []TeamRef{{Slug: "platform"}}}
			if in.EventThrottleRate != nil {
				p.EventThrottleRate = *in.EventThrottleRate
			}
			projects["checkout"] = p
			_ = json.NewEncoder(w).Encode(p)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/projects/acme/checkout/":
			p, ok := projects["checkout"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(p)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/projects/acme/checkout/":
			var in UpdateProjectRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			p := projects["checkout"]
			if in.Name != nil {
				p.Name = *in.Name
			}
			if in.EventThrottleRate != nil {
				p.EventThrottleRate = *in.EventThrottleRate
			}
			projects["checkout"] = p
			_ = json.NewEncoder(w).Encode(p)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/projects/acme/checkout/":
			delete(projects, "checkout")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	created, err := c.CreateProject(ctx, "acme", "platform", CreateProjectRequest{Name: "Checkout", Platform: "python"})
	if err != nil || created.Slug != "checkout" || len(created.Teams) != 1 || created.Teams[0].Slug != "platform" {
		t.Fatalf("create: %+v, err=%v", created, err)
	}

	got, err := c.GetProject(ctx, "acme", "checkout")
	if err != nil || got.Name != "Checkout" {
		t.Fatalf("get: %+v, err=%v", got, err)
	}

	rate := 0.5
	updated, err := c.UpdateProject(ctx, "acme", "checkout", UpdateProjectRequest{EventThrottleRate: &rate})
	if err != nil || updated.EventThrottleRate != 0.5 {
		t.Fatalf("update: %+v, err=%v", updated, err)
	}

	if err := c.DeleteProject(ctx, "acme", "checkout"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = c.GetProject(ctx, "acme", "checkout")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}

// TestCreateProjectSendsEventThrottleRate is a regression guard for a bug
// where CreateProjectRequest had no EventThrottleRate field at all, so a
// non-zero rate set on create was silently dropped rather than sent to the
// API. It asserts both that the field is present on the wire and that the
// server's response value round-trips back onto the created Project.
func TestCreateProjectSendsEventThrottleRate(t *testing.T) {
	var gotRaw map[string]any
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/0/teams/acme/platform/projects/" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotRaw)
		_ = json.NewEncoder(w).Encode(Project{ID: "100", Slug: "checkout", Name: "Checkout",
			EventThrottleRate: 10, Teams: []TeamRef{{Slug: "platform"}}})
	})
	defer srv.Close()
	c := newTestClient(t, srv)

	rate := 10.0
	created, err := c.CreateProject(context.Background(), "acme", "platform",
		CreateProjectRequest{Name: "Checkout", EventThrottleRate: &rate})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	sent, ok := gotRaw["eventThrottleRate"]
	if !ok {
		t.Fatalf("eventThrottleRate was not sent in the create request body: %+v", gotRaw)
	}
	if sent != 10.0 {
		t.Fatalf("eventThrottleRate sent = %v, want 10", sent)
	}
	if created.EventThrottleRate != 10 {
		t.Fatalf("created.EventThrottleRate = %v, want 10", created.EventThrottleRate)
	}
}

func TestListProjects(t *testing.T) {
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/0/organizations/acme/projects/" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]Project{{ID: "100", Slug: "checkout", Name: "Checkout"}})
	})
	defer srv.Close()
	c := newTestClient(t, srv)

	projects, next, err := c.ListProjects(context.Background(), "acme", "")
	if err != nil || len(projects) != 1 || next != "" {
		t.Fatalf("projects=%+v next=%q err=%v", projects, next, err)
	}
}
