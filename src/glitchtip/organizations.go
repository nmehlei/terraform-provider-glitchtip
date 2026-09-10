// src/glitchtip/organizations.go
package glitchtip

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

const organizationsPath = "organizations/"

// Organization is a GlitchTip organization.
type Organization struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// ListOrganizations fetches one page of organizations. Pass an empty
// cursorURL for the first page and a previous call's returned nextURL for
// subsequent pages; the client never constructs cursor values itself.
func (c *Client) ListOrganizations(ctx context.Context, cursorURL string) ([]Organization, string, error) {
	path := cursorURL
	if path == "" {
		path = organizationsPath
	}
	var orgs []Organization
	next, err := c.doPage(ctx, http.MethodGet, path, &orgs)
	if err != nil {
		return nil, "", err
	}
	return orgs, next, nil
}

// CreateOrganizationRequest is the writable subset of Organization
// accepted on create.
type CreateOrganizationRequest struct {
	Name string `json:"name"`
}

// CreateOrganization creates an organization and returns the server's
// representation of it.
func (c *Client) CreateOrganization(ctx context.Context, in CreateOrganizationRequest) (*Organization, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out Organization
	if err := c.do(ctx, http.MethodPost, organizationsPath, bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetOrganization retrieves an organization by slug.
func (c *Client) GetOrganization(ctx context.Context, slug string) (*Organization, error) {
	var out Organization
	if err := c.do(ctx, http.MethodGet, organizationsPath+slug+"/", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateOrganizationRequest is a partial update: only non-nil fields are
// sent, so unset fields are left unchanged on the server.
type UpdateOrganizationRequest struct {
	Name *string `json:"name,omitempty"`
}

// UpdateOrganization partially updates an organization and returns the
// resulting representation.
func (c *Client) UpdateOrganization(ctx context.Context, slug string, in UpdateOrganizationRequest) (*Organization, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out Organization
	if err := c.do(ctx, http.MethodPut, organizationsPath+slug+"/", bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteOrganization deletes an organization by slug.
func (c *Client) DeleteOrganization(ctx context.Context, slug string) error {
	return c.do(ctx, http.MethodDelete, organizationsPath+slug+"/", nil, nil)
}
