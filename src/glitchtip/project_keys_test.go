// src/glitchtip/project_keys_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateGetUpdateDeleteProjectKey(t *testing.T) {
	keys := map[string]ProjectKey{}
	base := "/api/0/projects/acme/checkout/keys/"
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			var in CreateProjectKeyRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			key := ProjectKey{ID: "key1", Name: in.Name, DSN: DSN{
				Public: "https://public@acme.glitchtip.example/100", Secret: "https://secret@acme.glitchtip.example/100",
			}}
			keys["key1"] = key
			_ = json.NewEncoder(w).Encode(key)
		case r.Method == http.MethodGet && r.URL.Path == base+"key1/":
			key, ok := keys["key1"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(key)
		case r.Method == http.MethodPut && r.URL.Path == base+"key1/":
			var in UpdateProjectKeyRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			key := keys["key1"]
			if in.Name != nil {
				key.Name = *in.Name
			}
			keys["key1"] = key
			_ = json.NewEncoder(w).Encode(key)
		case r.Method == http.MethodDelete && r.URL.Path == base+"key1/":
			delete(keys, "key1")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	created, err := c.CreateProjectKey(ctx, "acme", "checkout", CreateProjectKeyRequest{Name: "default"})
	if err != nil || created.ID != "key1" || created.DSN.Public == "" {
		t.Fatalf("create: %+v, err=%v", created, err)
	}

	got, err := c.GetProjectKey(ctx, "acme", "checkout", "key1")
	if err != nil || got.Name != "default" {
		t.Fatalf("get: %+v, err=%v", got, err)
	}

	newName := "renamed"
	updated, err := c.UpdateProjectKey(ctx, "acme", "checkout", "key1", UpdateProjectKeyRequest{Name: &newName})
	if err != nil || updated.Name != "renamed" {
		t.Fatalf("update: %+v, err=%v", updated, err)
	}

	if err := c.DeleteProjectKey(ctx, "acme", "checkout", "key1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = c.GetProjectKey(ctx, "acme", "checkout", "key1")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}
