// src/glitchtip/project_alerts_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestProjectAlertLifecycle(t *testing.T) {
	alerts := map[int]ProjectAlert{}
	base := "/api/0/projects/acme/checkout/alerts/"
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			var in ProjectAlertRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			a := ProjectAlert{ID: 7, Name: in.Name, TimespanMinutes: in.TimespanMinutes, Quantity: in.Quantity, Uptime: in.Uptime, Recipients: in.Recipients}
			alerts[7] = a
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(a)
		case r.Method == http.MethodGet && r.URL.Path == base:
			list := []ProjectAlert{}
			for _, a := range alerts {
				list = append(list, a)
			}
			_ = json.NewEncoder(w).Encode(list)
		case r.Method == http.MethodPut && r.URL.Path == base+"7/":
			var in ProjectAlertRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			a := ProjectAlert{ID: 7, Name: in.Name, TimespanMinutes: in.TimespanMinutes, Quantity: in.Quantity, Uptime: in.Uptime, Recipients: in.Recipients}
			alerts[7] = a
			_ = json.NewEncoder(w).Encode(a)
		case r.Method == http.MethodDelete && r.URL.Path == base+"7/":
			delete(alerts, 7)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	created, err := c.CreateProjectAlert(ctx, "acme", "checkout", ProjectAlertRequest{
		Name: "new issues", TimespanMinutes: 1, Quantity: 1, Recipients: []AlertRecipient{{RecipientType: "email"}},
	})
	if err != nil || created.ID != 7 || len(created.Recipients) != 1 {
		t.Fatalf("create: %+v, err=%v", created, err)
	}

	got, err := c.GetProjectAlert(ctx, "acme", "checkout", 7)
	if err != nil || got.Name != "new issues" {
		t.Fatalf("get: %+v, err=%v", got, err)
	}

	updated, err := c.UpdateProjectAlert(ctx, "acme", "checkout", 7, ProjectAlertRequest{
		Name: "new issues", TimespanMinutes: 5, Quantity: 3,
		Recipients: []AlertRecipient{{RecipientType: "email"}, {RecipientType: "ntfy", URL: "https://ntfy.example/wildberry"}},
	})
	if err != nil || updated.Quantity != 3 || len(updated.Recipients) != 2 {
		t.Fatalf("update: %+v, err=%v", updated, err)
	}

	if err := c.DeleteProjectAlert(ctx, "acme", "checkout", 7); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = c.GetProjectAlert(ctx, "acme", "checkout", 7)
	var apiErr *APIError
	if err == nil || !errorsAs(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("get after delete should be a 404 APIError, got %v", err)
	}
}
