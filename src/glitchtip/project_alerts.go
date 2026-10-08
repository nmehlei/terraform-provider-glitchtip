// src/glitchtip/project_alerts.go
package glitchtip

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

// AlertRecipient is one destination of a project alert: an email to the
// project's members (RecipientType "email", URL empty) or a webhook-style
// chat/notification URL (discord, feishu, webhook, googlechat, teams, ntfy).
type AlertRecipient struct {
	RecipientType string `json:"recipientType"`
	URL           string `json:"url,omitempty"`
}

// ProjectAlert is a GlitchTip project alert: send to the recipients when
// Quantity events happen within TimespanMinutes (or on an uptime failure).
type ProjectAlert struct {
	ID              int              `json:"id"`
	Name            string           `json:"name"`
	TimespanMinutes int              `json:"timespanMinutes"`
	Quantity        int              `json:"quantity"`
	Uptime          bool             `json:"uptime"`
	Recipients      []AlertRecipient `json:"alertRecipients"`
}

func projectAlertsPath(orgSlug, projectSlug string) string {
	return "projects/" + orgSlug + "/" + projectSlug + "/alerts/"
}
func projectAlertPath(orgSlug, projectSlug string, id int) string {
	return projectAlertsPath(orgSlug, projectSlug) + strconv.Itoa(id) + "/"
}

// ProjectAlertRequest is the writable shape for create and update (the API
// replaces the whole alert on PUT, so both use the same body).
type ProjectAlertRequest struct {
	Name            string           `json:"name"`
	TimespanMinutes int              `json:"timespanMinutes"`
	Quantity        int              `json:"quantity"`
	Uptime          bool             `json:"uptime"`
	Recipients      []AlertRecipient `json:"alertRecipients"`
}

// ListProjectAlerts returns every alert of a project.
func (c *Client) ListProjectAlerts(ctx context.Context, orgSlug, projectSlug string) ([]ProjectAlert, error) {
	var out []ProjectAlert
	path := projectAlertsPath(orgSlug, projectSlug)
	for path != "" {
		var page []ProjectAlert
		next, err := c.doPage(ctx, http.MethodGet, path, &page)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		path = next
	}
	return out, nil
}

// GetProjectAlert finds one alert by ID. The API has no single-alert GET, so
// it lists the project's alerts; a missing alert is reported as a 404 APIError
// so callers can treat it like any other deleted-out-of-band resource.
func (c *Client) GetProjectAlert(ctx context.Context, orgSlug, projectSlug string, id int) (*ProjectAlert, error) {
	alerts, err := c.ListProjectAlerts(ctx, orgSlug, projectSlug)
	if err != nil {
		return nil, err
	}
	for i := range alerts {
		if alerts[i].ID == id {
			return &alerts[i], nil
		}
	}
	return nil, &APIError{Method: http.MethodGet, URL: projectAlertPath(orgSlug, projectSlug, id), StatusCode: http.StatusNotFound, Body: "alert not found"}
}

// CreateProjectAlert creates an alert.
func (c *Client) CreateProjectAlert(ctx context.Context, orgSlug, projectSlug string, in ProjectAlertRequest) (*ProjectAlert, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out ProjectAlert
	if err := c.do(ctx, http.MethodPost, projectAlertsPath(orgSlug, projectSlug), bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateProjectAlert replaces an alert's settings.
func (c *Client) UpdateProjectAlert(ctx context.Context, orgSlug, projectSlug string, id int, in ProjectAlertRequest) (*ProjectAlert, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out ProjectAlert
	if err := c.do(ctx, http.MethodPut, projectAlertPath(orgSlug, projectSlug, id), bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProjectAlert deletes an alert.
func (c *Client) DeleteProjectAlert(ctx context.Context, orgSlug, projectSlug string, id int) error {
	return c.do(ctx, http.MethodDelete, projectAlertPath(orgSlug, projectSlug, id), nil, nil)
}
