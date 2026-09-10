// src/glitchtip/teams.go
package glitchtip

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// Team is a GlitchTip team, scoped to an organization.
type Team struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

func teamsListPath(orgSlug string) string      { return "organizations/" + orgSlug + "/teams/" }
func teamPath(orgSlug, teamSlug string) string { return "teams/" + orgSlug + "/" + teamSlug + "/" }

// ListTeams fetches one page of an organization's teams.
func (c *Client) ListTeams(ctx context.Context, orgSlug, cursorURL string) ([]Team, string, error) {
	path := cursorURL
	if path == "" {
		path = teamsListPath(orgSlug)
	}
	var teams []Team
	next, err := c.doPage(ctx, http.MethodGet, path, &teams)
	if err != nil {
		return nil, "", err
	}
	return teams, next, nil
}

// CreateTeamRequest is the writable subset of Team accepted on create.
type CreateTeamRequest struct {
	Slug string `json:"slug"`
}

// CreateTeam creates a team within orgSlug.
func (c *Client) CreateTeam(ctx context.Context, orgSlug string, in CreateTeamRequest) (*Team, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out Team
	if err := c.do(ctx, http.MethodPost, teamsListPath(orgSlug), bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTeam retrieves a team by organization and team slug.
func (c *Client) GetTeam(ctx context.Context, orgSlug, teamSlug string) (*Team, error) {
	var out Team
	if err := c.do(ctx, http.MethodGet, teamPath(orgSlug, teamSlug), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateTeamRequest is a partial update.
type UpdateTeamRequest struct {
	Slug *string `json:"slug,omitempty"`
}

// UpdateTeam partially updates a team.
func (c *Client) UpdateTeam(ctx context.Context, orgSlug, teamSlug string, in UpdateTeamRequest) (*Team, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out Team
	if err := c.do(ctx, http.MethodPut, teamPath(orgSlug, teamSlug), bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteTeam deletes a team by organization and team slug.
func (c *Client) DeleteTeam(ctx context.Context, orgSlug, teamSlug string) error {
	return c.do(ctx, http.MethodDelete, teamPath(orgSlug, teamSlug), nil, nil)
}
