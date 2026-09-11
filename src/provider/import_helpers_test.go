// src/provider/import_helpers_test.go
package provider

import "testing"

func TestSplitImportID(t *testing.T) {
	parts, err := splitImportID("acme:platform", 2)
	if err != nil || parts[0] != "acme" || parts[1] != "platform" {
		t.Fatalf("parts=%v err=%v", parts, err)
	}
	if _, err := splitImportID("acme:platform", 3); err == nil {
		t.Fatal("expected error for wrong segment count")
	}
	if _, err := splitImportID("acme", 2); err == nil {
		t.Fatal("expected error for too few segments")
	}
}
