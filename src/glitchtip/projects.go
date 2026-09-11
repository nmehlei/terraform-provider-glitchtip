// src/glitchtip/projects.go
package glitchtip

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// TeamRef is the minimal team reference embedded in a Project's Teams list.
type TeamRef struct {
	Slug string `json:"slug"`
}

// Project is a GlitchTip project, scoped to an organization and associated
// with one or more teams.
type Project struct {
	ID                string    `json:"id"`
	Slug              string    `json:"slug"`
	Name              string    `json:"name"`
	Platform          string    `json:"platform"`
	EventThrottleRate float64   `json:"eventThrottleRate"`
	Teams             []TeamRef `json:"teams"`
}

func projectsListPath(orgSlug string) string { return "organizations/" + orgSlug + "/projects/" }
func projectsCreatePath(orgSlug, teamSlug string) string {
	return "teams/" + orgSlug + "/" + teamSlug + "/projects/"
}
func projectPath(orgSlug, projectSlug string) string {
	return "projects/" + orgSlug + "/" + projectSlug + "/"
}

// ListProjects fetches one page of an organization's projects.
func (c *Client) ListProjects(ctx context.Context, orgSlug, cursorURL string) ([]Project, string, error) {
	path := cursorURL
	if path == "" {
		path = projectsListPath(orgSlug)
	}
	var projects []Project
	next, err := c.doPage(ctx, http.MethodGet, path, &projects)
	if err != nil {
		return nil, "", err
	}
	return projects, next, nil
}

// CreateProjectRequest is the writable subset of Project accepted on
// create. The project is created under teamSlug (see CreateProject); Platform
// may be empty for GlitchTip's generic/"other" platform.
type CreateProjectRequest struct {
	Name              string   `json:"name"`
	Platform          string   `json:"platform,omitempty"`
	EventThrottleRate *float64 `json:"eventThrottleRate,omitempty"`
}

// CreateProject creates a project under the given team. The team a project
// is created under becomes its initial team membership — see Project.Teams
// and ProjectTeamMembership for adding further teams afterward.
func (c *Client) CreateProject(ctx context.Context, orgSlug, teamSlug string, in CreateProjectRequest) (*Project, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out Project
	if err := c.do(ctx, http.MethodPost, projectsCreatePath(orgSlug, teamSlug), bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetProject retrieves a project by organization and project slug.
func (c *Client) GetProject(ctx context.Context, orgSlug, projectSlug string) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodGet, projectPath(orgSlug, projectSlug), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateProjectRequest is a partial update.
type UpdateProjectRequest struct {
	Name              *string  `json:"name,omitempty"`
	Platform          *string  `json:"platform,omitempty"`
	EventThrottleRate *float64 `json:"eventThrottleRate,omitempty"`
}

// UpdateProject partially updates a project.
func (c *Client) UpdateProject(ctx context.Context, orgSlug, projectSlug string, in UpdateProjectRequest) (*Project, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out Project
	if err := c.do(ctx, http.MethodPut, projectPath(orgSlug, projectSlug), bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProject deletes a project by organization and project slug.
func (c *Client) DeleteProject(ctx context.Context, orgSlug, projectSlug string) error {
	return c.do(ctx, http.MethodDelete, projectPath(orgSlug, projectSlug), nil, nil)
}
