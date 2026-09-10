// src/glitchtip/project_team_memberships.go
package glitchtip

import (
	"context"
	"net/http"
)

func projectTeamPath(orgSlug, projectSlug, teamSlug string) string {
	return "projects/" + orgSlug + "/" + projectSlug + "/teams/" + teamSlug + "/"
}

func projectTeamsListPath(orgSlug, projectSlug string) string {
	return "projects/" + orgSlug + "/" + projectSlug + "/teams/"
}

// AssignProjectTeam associates teamSlug with projectSlug. Idempotent server-side.
func (c *Client) AssignProjectTeam(ctx context.Context, orgSlug, projectSlug, teamSlug string) error {
	return c.do(ctx, http.MethodPost, projectTeamPath(orgSlug, projectSlug, teamSlug), nil, nil)
}

// RemoveProjectTeam disassociates teamSlug from projectSlug.
func (c *Client) RemoveProjectTeam(ctx context.Context, orgSlug, projectSlug, teamSlug string) error {
	return c.do(ctx, http.MethodDelete, projectTeamPath(orgSlug, projectSlug, teamSlug), nil, nil)
}

// ListProjectTeams returns the teams currently associated with a project.
// Uses the dedicated GET .../teams/ endpoint (a bare JSON array of team
// objects) because the single-project GET response omits the teams list.
func (c *Client) ListProjectTeams(ctx context.Context, orgSlug, projectSlug string) ([]TeamRef, error) {
	var teams []TeamRef
	if err := c.do(ctx, http.MethodGet, projectTeamsListPath(orgSlug, projectSlug), nil, &teams); err != nil {
		return nil, err
	}
	return teams, nil
}

// GetProjectTeamMembership reports whether teamSlug is currently associated
// with projectSlug. There is no single-membership GET endpoint, so this
// lists the project's teams and checks membership; when the team isn't
// present it returns a synthesized *APIError with StatusCode 404 so
// NotFound() behaves the same as every other resource's Read path.
func (c *Client) GetProjectTeamMembership(ctx context.Context, orgSlug, projectSlug, teamSlug string) error {
	teams, err := c.ListProjectTeams(ctx, orgSlug, projectSlug)
	if err != nil {
		return err
	}
	for _, tm := range teams {
		if tm.Slug == teamSlug {
			return nil
		}
	}
	return &APIError{
		Method:     http.MethodGet,
		URL:        projectTeamPath(orgSlug, projectSlug, teamSlug),
		StatusCode: 404,
		Body:       "team " + teamSlug + " is not associated with project " + projectSlug,
	}
}
