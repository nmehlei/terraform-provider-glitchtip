// src/glitchtip/project_team_memberships_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAssignGetRemoveProjectTeam(t *testing.T) {
	teams := []TeamRef{{Slug: "platform"}}
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/projects/acme/checkout/teams/sre/":
			teams = append(teams, TeamRef{Slug: "sre"})
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/projects/acme/checkout/teams/sre/":
			kept := teams[:0]
			for _, tm := range teams {
				if tm.Slug != "sre" {
					kept = append(kept, tm)
				}
			}
			teams = kept
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/projects/acme/checkout/teams/":
			_ = json.NewEncoder(w).Encode(teams)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	// Not yet assigned.
	err := c.GetProjectTeamMembership(ctx, "acme", "checkout", "sre")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("expected NotFound before assign, got %v", err)
	}

	if err := c.AssignProjectTeam(ctx, "acme", "checkout", "sre"); err != nil {
		t.Fatalf("assign: %v", err)
	}

	if err := c.GetProjectTeamMembership(ctx, "acme", "checkout", "sre"); err != nil {
		t.Fatalf("expected membership to exist after assign, got %v", err)
	}

	if err := c.RemoveProjectTeam(ctx, "acme", "checkout", "sre"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	err = c.GetProjectTeamMembership(ctx, "acme", "checkout", "sre")
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("expected NotFound after remove, got %v", err)
	}
}
