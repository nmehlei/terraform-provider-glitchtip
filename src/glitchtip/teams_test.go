// src/glitchtip/teams_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateGetUpdateDeleteTeam(t *testing.T) {
	teams := map[string]Team{}
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/organizations/acme/teams/":
			var in CreateTeamRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			team := Team{ID: "10", Slug: in.Slug}
			teams["platform"] = team
			_ = json.NewEncoder(w).Encode(team)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/teams/acme/platform/":
			team, ok := teams["platform"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(team)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/teams/acme/platform/":
			var in UpdateTeamRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			team := teams["platform"]
			if in.Slug != nil {
				team.Slug = *in.Slug
				// Update the key in the map to match new slug
				delete(teams, "platform")
				teams[*in.Slug] = team
			}
			_ = json.NewEncoder(w).Encode(team)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/teams/acme/platform/":
			delete(teams, "platform")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	created, err := c.CreateTeam(ctx, "acme", CreateTeamRequest{Slug: "platform"})
	if err != nil || created.Slug != "platform" {
		t.Fatalf("create: %+v, err=%v", created, err)
	}

	got, err := c.GetTeam(ctx, "acme", "platform")
	if err != nil || got.Slug != "platform" {
		t.Fatalf("get: %+v, err=%v", got, err)
	}

	newSlug := "platform-engineering"
	updated, err := c.UpdateTeam(ctx, "acme", "platform", UpdateTeamRequest{Slug: &newSlug})
	if err != nil || updated.Slug != "platform-engineering" {
		t.Fatalf("update: %+v, err=%v", updated, err)
	}

	if err := c.DeleteTeam(ctx, "acme", "platform"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = c.GetTeam(ctx, "acme", "platform")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}

func TestListTeams(t *testing.T) {
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/0/organizations/acme/teams/" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]Team{{ID: "10", Slug: "platform"}})
	})
	defer srv.Close()
	c := newTestClient(t, srv)

	teams, next, err := c.ListTeams(context.Background(), "acme", "")
	if err != nil || len(teams) != 1 || next != "" {
		t.Fatalf("teams=%+v next=%q err=%v", teams, next, err)
	}
}
