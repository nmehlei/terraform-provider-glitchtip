// src/glitchtip/project_keys.go
package glitchtip

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// DSN is the set of DSNs a project key exposes. Public is the one clients
// send events with; Secret and Security are legacy/optional variants some
// SDKs still read.
type DSN struct {
	Public   string `json:"public"`
	Secret   string `json:"secret,omitempty"`
	Security string `json:"security,omitempty"`
}

// ProjectKey is a DSN-bearing key for a project.
type ProjectKey struct {
	ID   string `json:"id"`
	Name string `json:"label"`
	DSN  DSN    `json:"dsn"`
}

func projectKeysListPath(orgSlug, projectSlug string) string {
	return "projects/" + orgSlug + "/" + projectSlug + "/keys/"
}
func projectKeyPath(orgSlug, projectSlug, keyID string) string {
	return projectKeysListPath(orgSlug, projectSlug) + keyID + "/"
}

// ListProjectKeys fetches one page of a project's keys.
func (c *Client) ListProjectKeys(ctx context.Context, orgSlug, projectSlug, cursorURL string) ([]ProjectKey, string, error) {
	path := cursorURL
	if path == "" {
		path = projectKeysListPath(orgSlug, projectSlug)
	}
	var keys []ProjectKey
	next, err := c.doPage(ctx, http.MethodGet, path, &keys)
	if err != nil {
		return nil, "", err
	}
	return keys, next, nil
}

// CreateProjectKeyRequest is the writable subset of ProjectKey accepted on
// create. Name maps to the API's "label" field (see ProjectKey.Name's json tag).
type CreateProjectKeyRequest struct {
	Name string `json:"name"`
}

// CreateProjectKey creates a new DSN-bearing key for a project.
func (c *Client) CreateProjectKey(ctx context.Context, orgSlug, projectSlug string, in CreateProjectKeyRequest) (*ProjectKey, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out ProjectKey
	if err := c.do(ctx, http.MethodPost, projectKeysListPath(orgSlug, projectSlug), bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetProjectKey retrieves a single project key by ID.
func (c *Client) GetProjectKey(ctx context.Context, orgSlug, projectSlug, keyID string) (*ProjectKey, error) {
	var out ProjectKey
	if err := c.do(ctx, http.MethodGet, projectKeyPath(orgSlug, projectSlug, keyID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateProjectKeyRequest is a partial update.
type UpdateProjectKeyRequest struct {
	Name *string `json:"name,omitempty"`
}

// UpdateProjectKey partially updates a project key (only its name is
// writable; DSNs are server-assigned).
func (c *Client) UpdateProjectKey(ctx context.Context, orgSlug, projectSlug, keyID string, in UpdateProjectKeyRequest) (*ProjectKey, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out ProjectKey
	if err := c.do(ctx, http.MethodPut, projectKeyPath(orgSlug, projectSlug, keyID), bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProjectKey deletes a project key by ID.
func (c *Client) DeleteProjectKey(ctx context.Context, orgSlug, projectSlug, keyID string) error {
	return c.do(ctx, http.MethodDelete, projectKeyPath(orgSlug, projectSlug, keyID), nil, nil)
}
