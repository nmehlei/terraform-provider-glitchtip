// src/provider/docs_test.go
package provider

import (
	"os/exec"
	"testing"
)

// TestDocsAreGenerated regenerates the Registry docs and fails if the
// working tree changed, so a schema edit that forgets `go generate` is
// caught in CI rather than shipped.
func TestDocsAreGenerated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docs generation check in -short mode")
	}
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform binary not on PATH; skipping docs check")
	}
	// This test file lives in src/provider; the //go:generate directive is
	// in src/main.go, and the generated docs land at the repo root (docs/).
	gen := exec.Command("go", "generate", "./...")
	gen.Dir = ".." // src/
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("go generate failed: %v\n%s", err, out)
	}
	// Scope the diff to the generated pages only — docs/superpowers/ holds
	// the hand-written spec and plan and must not be swept in.
	diff := exec.Command("git", "diff", "--exit-code", "--",
		"../../docs/index.md", "../../docs/resources")
	if out, err := diff.CombinedOutput(); err != nil {
		t.Fatalf("generated docs are out of date; run `cd src && go generate ./...` and commit:\n%s", out)
	}
}
