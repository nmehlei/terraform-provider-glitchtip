// src/provider/import_helpers.go
package provider

import (
	"fmt"
	"strings"
)

// splitImportID splits a colon-delimited terraform import identifier into
// exactly want non-empty segments, or returns an error describing the
// expected shape. Each resource that uses a compound import id documents
// its own segment order in its schema description.
func splitImportID(id string, want int) ([]string, error) {
	parts := strings.Split(id, ":")
	if len(parts) != want {
		return nil, fmt.Errorf("expected an import identifier with %d colon-separated segments, got %d in %q", want, len(parts), id)
	}
	for i, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("import identifier segment %d is empty in %q", i+1, id)
		}
	}
	return parts, nil
}
