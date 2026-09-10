# terraform-provider-glitchtip Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a working Terraform provider (`nmehlei/glitchtip`) managing GlitchTip organizations, teams, projects, project keys (DSNs), and project↔team membership, with full CRUD, import, acceptance tests against a real disposable GlitchTip instance, and a signed release pipeline.

**Architecture:** Hand-written HTTP client (`src/glitchtip`, zero Terraform deps) wrapped by five terraform-plugin-framework resources (`src/provider`). Client wire format is verified against a live GlitchTip instance before any resource code is written. Testing is real-instance-only (Docker), no mock server.

**Tech Stack:** Go, terraform-plugin-framework v1.19.0, terraform-plugin-docs (tfplugindocs), GitVersion 5.x (Mainline), GoReleaser, Docker Compose (GlitchTip).

**Spec:** `docs/superpowers/specs/2026-09-10-terraform-provider-glitchtip-design.md`

## Global Constraints

- Module: `github.com/nmehlei/terraform-provider-glitchtip`, Go 1.25+.
- Framework: terraform-plugin-framework v1.19.0 only — never SDKv2.
- Provider address: `registry.terraform.io/nmehlei/glitchtip`.
- Default `endpoint`: `https://app.glitchtip.com`; override for self-hosted.
- Token env var fallback: `GLITCHTIP_TOKEN`. `token` attribute is `Sensitive`.
- `dsn_public` / `dsn_security` on `glitchtip_project_key` are `Computed` + `Sensitive`.
- Layout: `src/` for Go source, `tests/` for tests — not `internal/`.
- No mock API server. All non-unit verification runs against a real disposable GlitchTip instance via Docker Compose.
- License: Apache-2.0.
- Every exported client/resource method gets a doc comment; every schema attribute gets a `MarkdownDescription` (feeds generated Registry docs).

---

## Task 1: Verify the GlitchTip wire format against a live instance

**Files:**
- Create: `docs/api-notes.md`
- Create: `tests/acceptance/docker-compose.yml`
- Create: `tests/acceptance/up.sh`
- Create: `tests/acceptance/down.sh`

**Interfaces:**
- Produces: a running local GlitchTip instance (`http://localhost:8000`), an auth token, and `docs/api-notes.md` recording actual observed request/response JSON for every endpoint Task 2 onward depends on. Every later client task cites this file for field names.

This task exists because the design doc flags GlitchTip's exact field names (`platform`, `event_throttle_rate`, project-key DSN shape, pagination) as assumed-from-docs, not observed. Ground every later task in reality before writing client code against it.

- [ ] **Step 1: Write the Compose file**

```yaml
# tests/acceptance/docker-compose.yml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_DB: glitchtip
      POSTGRES_PASSWORD: glitchtip
      POSTGRES_USER: glitchtip
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U glitchtip"]
      interval: 2s
      timeout: 5s
      retries: 30

  redis:
    image: redis:7

  web:
    image: glitchtip/glitchtip:latest
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_started
    ports:
      - "8000:8000"
    environment:
      DATABASE_URL: postgres://glitchtip:glitchtip@postgres:5432/glitchtip
      SECRET_KEY: acceptance-test-secret-key
      REDIS_URL: redis://redis:6379/0
      PORT: "8000"
      CELERY_WORKER_AUTOSCALE: "1,1"
      ENABLE_OPEN_USER_REGISTRATION: "true"
      GLITCHTIP_DOMAIN: http://localhost:8000
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8000/_health/"]
      interval: 3s
      timeout: 5s
      retries: 40

  worker:
    image: glitchtip/glitchtip:latest
    command: ./bin/run-celery-with-beat.sh
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_started
    environment:
      DATABASE_URL: postgres://glitchtip:glitchtip@postgres:5432/glitchtip
      SECRET_KEY: acceptance-test-secret-key
      REDIS_URL: redis://redis:6379/0
```

- [ ] **Step 2: Write `up.sh` / `down.sh`**

```bash
#!/usr/bin/env bash
# tests/acceptance/up.sh
set -euo pipefail
cd "$(dirname "$0")"
docker compose up -d
echo "Waiting for GlitchTip to become healthy..."
timeout 180 bash -c 'until [ "$(docker compose ps -q web | xargs docker inspect -f "{{.State.Health.Status}}")" = "healthy" ]; do sleep 2; done'
echo "GlitchTip is up at http://localhost:8000"
```

```bash
#!/usr/bin/env bash
# tests/acceptance/down.sh
set -euo pipefail
cd "$(dirname "$0")"
docker compose down -v
```

Make both executable: `chmod +x tests/acceptance/up.sh tests/acceptance/down.sh`.

- [ ] **Step 3: Bring the instance up and create an org + auth token**

Run: `tests/acceptance/up.sh`

Then, by hand (documented in `docs/api-notes.md`, not automated — this is a one-time exploration):
1. Open `http://localhost:8000`, register the first user (becomes superuser), which auto-creates an organization.
2. In the UI, create an API auth token (Settings → your account → Auth Tokens, or the org's API settings — record the exact path found).
3. Export it: `export GT_TOKEN=<token>`, `export GT_ORG=<the auto-created org slug>`.

- [ ] **Step 4: Probe every endpoint the design depends on, record real JSON**

```bash
# Organizations
curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/organizations/ | jq .
curl -sS -H "Authorization: Bearer $GT_TOKEN" "http://localhost:8000/api/0/organizations/$GT_ORG/" | jq .

# Teams: create then list/get
curl -sS -X POST -H "Authorization: Bearer $GT_TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"platform"}' \
  "http://localhost:8000/api/0/organizations/$GT_ORG/teams/" | jq .
curl -sS -H "Authorization: Bearer $GT_TOKEN" "http://localhost:8000/api/0/organizations/$GT_ORG/teams/" | jq .

# Projects: create under the team, then get
curl -sS -X POST -H "Authorization: Bearer $GT_TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"checkout","platform":"python"}' \
  "http://localhost:8000/api/0/teams/$GT_ORG/platform/projects/" | jq .
curl -sS -H "Authorization: Bearer $GT_TOKEN" "http://localhost:8000/api/0/projects/$GT_ORG/checkout/" | jq .

# Project keys
curl -sS -H "Authorization: Bearer $GT_TOKEN" "http://localhost:8000/api/0/projects/$GT_ORG/checkout/keys/" | jq .
curl -sS -X POST -H "Authorization: Bearer $GT_TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"default"}' \
  "http://localhost:8000/api/0/projects/$GT_ORG/checkout/keys/" | jq .

# Project-team membership: assign a second team, then check the project's teams
curl -sS -X POST -H "Authorization: Bearer $GT_TOKEN" \
  "http://localhost:8000/api/0/organizations/$GT_ORG/teams/" -d '{"name":"sre"}' \
  -H "Content-Type: application/json" | jq .
curl -sS -X POST -H "Authorization: Bearer $GT_TOKEN" \
  "http://localhost:8000/api/0/projects/$GT_ORG/checkout/teams/sre/" | jq .
curl -sS -H "Authorization: Bearer $GT_TOKEN" "http://localhost:8000/api/0/projects/$GT_ORG/checkout/" | jq '.teams'
curl -sS -X DELETE -H "Authorization: Bearer $GT_TOKEN" \
  "http://localhost:8000/api/0/projects/$GT_ORG/checkout/teams/sre/" -w '%{http_code}\n'

# 404 shape (for NotFound() detection)
curl -sS -H "Authorization: Bearer $GT_TOKEN" "http://localhost:8000/api/0/projects/$GT_ORG/does-not-exist/" -w '\n%{http_code}\n'

# Pagination header shape on a list with >1 page (skip if instance too small; otherwise:)
curl -sSI -H "Authorization: Bearer $GT_TOKEN" "http://localhost:8000/api/0/organizations/$GT_ORG/teams/" | grep -i '^link:'

# OpenAPI schema location
curl -sS -o /dev/null -w '%{http_code}\n' http://localhost:8000/api/openapi.json
```

Paste every actual request/response pair into `docs/api-notes.md` under headings per resource. Note the exact JSON keys, the DELETE status code, the 404 body shape, and whether `Link` pagination headers appear.

- [ ] **Step 5: Tear down and commit**

```bash
tests/acceptance/down.sh
git add docs/api-notes.md tests/acceptance/docker-compose.yml tests/acceptance/up.sh tests/acceptance/down.sh
git commit -m "docs: record observed GlitchTip API wire format

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

**Note for Tasks 2–8:** every field name, path, and status code below is written from GlitchTip/Sentry-compatible documentation as a starting point. Before implementing each client file, re-check its section of `docs/api-notes.md` from this task and adjust field names/paths to match what was actually observed — the code below is the design's best assumption, `docs/api-notes.md` is the authority once Task 1 is done.

---

## Task 2: Repo scaffolding

**Files:**
- Create: `go.mod`, `go.sum` (via `go mod tidy`)
- Create: `src/main.go`
- Create: `LICENSE` (Apache-2.0)
- Create: `.gitignore`
- Create: `AGENTS.md`
- Create: `GitVersion.yml`
- Create: `.goreleaser.yml`
- Create: `terraform-registry-manifest.json`

**Interfaces:**
- Produces: a `go build ./...` that compiles a provider binary with an empty resource/data-source list. `src/provider.New(version string) func() provider.Provider` — the factory every later task's `provider.go` (Task 9) replaces.

- [ ] **Step 1: Initialize the module and directories**

```bash
cd ~/Dev/terraform-provider-glitchtip
go mod init github.com/nmehlei/terraform-provider-glitchtip
mkdir -p src/provider src/glitchtip tests/unit tests/acceptance examples/provider docs/resources
```

- [ ] **Step 2: Add framework dependencies**

```bash
go get github.com/hashicorp/terraform-plugin-framework@v1.19.0
go get github.com/hashicorp/terraform-plugin-framework-validators@v0.19.0
go get github.com/hashicorp/terraform-plugin-go@v0.31.0
go get github.com/hashicorp/terraform-plugin-log@v0.11.0
go get github.com/hashicorp/terraform-exec@v0.25.3
go get github.com/hashicorp/terraform-json@v0.28.0
go get -tool github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0
```

- [ ] **Step 3: Write a placeholder provider package so `main.go` compiles**

```go
// src/provider/provider.go
// Package provider implements the GlitchTip Terraform provider.
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ provider.Provider = &GlitchTipProvider{}

// New returns a provider.Provider factory, as required by
// providerserver.Serve. version is injected at build time (see src/main.go).
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &GlitchTipProvider{version: version}
	}
}

type GlitchTipProvider struct {
	version string
}

func (p *GlitchTipProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "glitchtip"
	resp.Version = p.version
}

func (p *GlitchTipProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages resources on a GlitchTip instance's REST API (`/api/0/`).",
	}
}

func (p *GlitchTipProvider) Configure(_ context.Context, _ provider.ConfigureRequest, _ *provider.ConfigureResponse) {
}

func (p *GlitchTipProvider) Resources(_ context.Context) []func() resource.Resource {
	return nil
}

func (p *GlitchTipProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
```

- [ ] **Step 4: Write `main.go`**

```go
// src/main.go
package main

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-dir ..

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/nmehlei/terraform-provider-glitchtip/src/provider"
)

// version is set at build time (e.g. via -ldflags "-X main.version=...").
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/nmehlei/glitchtip",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
```

- [ ] **Step 5: `.gitignore`, `AGENTS.md`, `GitVersion.yml`, manifest**

```
# .gitignore
terraform-provider-glitchtip
terraform-provider-glitchtip_v*
*.zip
.terraform/
.terraform.lock.hcl
*.tfstate
*.tfstate.*
```

```markdown
# AGENTS.md
# Terraform Provider for GlitchTip

## Mission

Build a public, independent Terraform provider for GlitchTip. Use Go and
HashiCorp's Terraform Plugin Framework; do not use SDKv2 for new code.

## First action

Read `docs/superpowers/specs/2026-09-10-terraform-provider-glitchtip-design.md`
and `docs/api-notes.md`.

## Security and testing

- Accept API credentials only through a sensitive provider attribute or
  `GLITCHTIP_TOKEN`; never log, return, or write them to fixtures.
- Treat DSNs as sensitive state.
- Use disposable Docker GlitchTip instances for acceptance tests. Never
  point tests at a shared or production instance.

## Repository conventions

- Put Go source under `src/` and tests under `tests/`.
- Use semantic versioning, generated Registry documentation, signed
  releases, and a public GitHub release workflow.

## Completion gate for v1

Do not publish until every resource has tested create/read/update/delete,
import, drift detection, and acceptance tests.
```

```yaml
# GitVersion.yml
mode: Mainline
branches:
  main:
    regex: ^main$
    increment: Patch
    is-mainline: true
```

```json
{
    "version": 1,
    "metadata": {
        "protocol_versions": ["6.0"]
    }
}
```
(save as `terraform-registry-manifest.json`)

- [ ] **Step 6: `.goreleaser.yml`**

```yaml
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - dir: src
    binary: "{{ .ProjectName }}_v{{ .Version }}"
    env:
      - CGO_ENABLED=0
    mod_timestamp: "{{ .CommitTimestamp }}"
    flags:
      - -trimpath
    ldflags:
      - -s -w -X main.version={{.Version}} -X main.commit={{.Commit}}
    goos:
      - darwin
      - linux
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64

archives:
  - format: zip
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  extra_files:
    - glob: "terraform-registry-manifest.json"
      name_template: "{{ .ProjectName }}_{{ .Version }}_manifest.json"
  name_template: "{{ .ProjectName }}_{{ .Version }}_SHA256SUMS"
  algorithm: sha256

signs:
  - artifacts: checksum
    args:
      - "--batch"
      - "--local-user={{ .Env.GPG_FINGERPRINT }}"
      - "--output=${signature}"
      - "--detach-sign"
      - "${artifact}"

release:
  extra_files:
    - glob: "terraform-registry-manifest.json"
      name_template: "{{ .ProjectName }}_{{ .Version }}_manifest.json"

changelog:
  disable: true
```

- [ ] **Step 7: Get an Apache-2.0 `LICENSE` file, build, verify**

Run: `curl -sS https://www.apache.org/licenses/LICENSE-2.0.txt -o LICENSE` (then insert `[yyyy] [name of copyright owner]` line with `2026 nmehlei` per the template's instructions section)

Run: `go build -o /tmp/tfp-glitchtip ./src && /tmp/tfp-glitchtip -help 2>&1 | head -1`
Expected: prints the plugin's usage/handshake output (protocol negotiation line), not an error.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum src/ LICENSE .gitignore AGENTS.md GitVersion.yml .goreleaser.yml terraform-registry-manifest.json
git commit -m "chore: scaffold provider skeleton

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---
## Task 3: Client core — `src/glitchtip/client.go`, `errors.go`

**Files:**
- Create: `src/glitchtip/client.go`
- Create: `src/glitchtip/errors.go`
- Test: `src/glitchtip/client_test.go`

**Interfaces:**
- Consumes: nothing (foundation package).
- Produces: `Config{Endpoint, Token, Timeout, AllowInsecure, HTTPClient}`, `New(ctx, cfg) (*Client, error)`, `(*Client) do(ctx, method, path string, body io.Reader, out any) error`, `(*Client) doPage(ctx, method, path string, out any) (nextURL string, err error)`, `*ConfigError{Msg string, Err error}`, `*APIError{Method, URL string, StatusCode int, Body string}` with `(*APIError) NotFound() bool` and `(*APIError) Error() string`. Every later client file (Tasks 4–8) calls `do`/`doPage` and returns `*APIError`/`*ConfigError`.

- [ ] **Step 1: Write `errors.go`**

```go
// src/glitchtip/errors.go
package glitchtip

import "fmt"

// ConfigError indicates a problem with local client configuration —
// a bad endpoint, missing credential, or unsupported API version. It is
// never returned for a problem on the server side; that's APIError.
type ConfigError struct {
	Msg string
	Err error
}

func (e *ConfigError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("glitchtip: %s: %v", e.Msg, e.Err)
	}
	return fmt.Sprintf("glitchtip: %s", e.Msg)
}

func (e *ConfigError) Unwrap() error { return e.Err }

// APIError is a non-2xx response from the GlitchTip API.
type APIError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("glitchtip: %s %s: unexpected status %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

// NotFound reports whether the server responded 404, the signal every
// resource's Read method uses to detect out-of-band deletion.
func (e *APIError) NotFound() bool {
	return e.StatusCode == 404
}
```

- [ ] **Step 2: Write `client_test.go` (unit tests against `httptest.Server`)**

```go
// src/glitchtip/client_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	return srv, srv.Close
}

func TestNew_RequiresEndpointAndToken(t *testing.T) {
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := New(context.Background(), Config{Endpoint: "https://example.com"}); err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestNew_RejectsPlainHTTPWithoutAllowInsecure(t *testing.T) {
	_, err := New(context.Background(), Config{Endpoint: "http://example.com", Token: "t"})
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *ConfigError, got %v (%T)", err, err)
	}
}

func TestNew_ChecksVersionAgainstOpenAPISchema(t *testing.T) {
	srv, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/openapi.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
	})
	defer closeFn()

	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "t", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected a client")
	}
}

func TestDo_DecodesJSONAndSetsAuthHeader(t *testing.T) {
	var gotAuth string
	srv, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]string{"slug": "acme"})
	})
	defer closeFn()

	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "secret-token", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out struct {
		Slug string `json:"slug"`
	}
	if err := c.do(context.Background(), http.MethodGet, "organizations/acme/", nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Slug != "acme" {
		t.Fatalf("expected slug acme, got %q", out.Slug)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("expected Bearer secret-token, got %q", gotAuth)
	}
}

func TestDo_ReturnsAPIErrorOnNon2xx(t *testing.T) {
	srv, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"not found"}`))
	})
	defer closeFn()

	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "t", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = c.do(context.Background(), http.MethodGet, "organizations/nope/", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v (%T)", err, err)
	}
	if !apiErr.NotFound() {
		t.Fatalf("expected NotFound() true, got status %d", apiErr.StatusCode)
	}
}

func TestDoPage_ParsesNextLinkHeader(t *testing.T) {
	srv, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		next := srv2URL(r) + `/api/0/organizations/?cursor=100:1:0`
		w.Header().Set("Link",
			`<`+next+`>; rel="next"; results="true"; cursor="100:1:0", `+
				`<`+next+`>; rel="previous"; results="false"; cursor="100:-1:1"`)
		_ = json.NewEncoder(w).Encode([]map[string]string{{"slug": "acme"}})
	})
	defer closeFn()

	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "t", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out []map[string]string
	next, err := c.doPage(context.Background(), http.MethodGet, "organizations/", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next == "" {
		t.Fatal("expected a non-empty next cursor URL")
	}
}

func TestDoPage_EmptyNextWhenResultsFalse(t *testing.T) {
	srv, closeFn := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		w.Header().Set("Link", `<https://x/y?cursor=1:1:0>; rel="next"; results="false"; cursor="1:1:0"`)
		_ = json.NewEncoder(w).Encode([]map[string]string{})
	})
	defer closeFn()

	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "t", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out []map[string]string
	next, err := c.doPage(context.Background(), http.MethodGet, "organizations/", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next != "" {
		t.Fatalf("expected empty next when results=false, got %q", next)
	}
}

// srv2URL is a tiny helper so the Link-header fixture above can embed a
// syntactically valid absolute URL without depending on the server's own
// address at handler-definition time.
func srv2URL(r *http.Request) string {
	scheme := "http"
	return scheme + "://" + r.Host
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./src/glitchtip/... -run . -v`
Expected: FAIL — `client.go` doesn't exist yet, compile error.

- [ ] **Step 4: Implement `client.go`**

```go
// src/glitchtip/client.go
// Package glitchtip is a minimal, hand-written HTTP client for a GlitchTip
// instance's REST API (/api/0/). It intentionally has no dependency on the
// Terraform Plugin Framework so it can be used and tested standalone.
package glitchtip

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	defaultTimeout = 30 * time.Second
	apiPrefix      = "api/0/"
	schemaPath     = "api/openapi.json"

	// maxResponseBody bounds how much of a response this client will ever
	// read into memory or embed in an error.
	maxResponseBody = 1 << 20 // 1 MiB
)

// Config configures a Client.
type Config struct {
	// Endpoint is the base URL of the GlitchTip instance, e.g.
	// "https://app.glitchtip.com". Must be HTTPS unless AllowInsecure is set.
	Endpoint string

	// Token authenticates every request as a bearer token.
	Token string

	// Timeout bounds every HTTP request. Defaults to 30s when zero.
	Timeout time.Duration

	// AllowInsecure permits a plain-HTTP endpoint. Intended only for local
	// development against a disposable instance.
	AllowInsecure bool

	// HTTPClient overrides the underlying HTTP client. Primarily for tests.
	HTTPClient *http.Client
}

// Client is a GlitchTip REST API client.
type Client struct {
	// baseURL is the resolved API root (endpoint + apiPrefix), with any
	// userinfo stripped so it can never leak into a request URL or error.
	baseURL    *url.URL
	rootURL    *url.URL // endpoint root, for schemaPath
	token      string
	timeout    time.Duration
	httpClient *http.Client
}

// New validates cfg, fetches and checks the remote OpenAPI schema is
// reachable, and returns a ready Client.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, &ConfigError{Msg: "endpoint is required"}
	}
	if cfg.Token == "" {
		return nil, &ConfigError{Msg: "token is required"}
	}

	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, &ConfigError{Msg: "invalid endpoint", Err: err}
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !cfg.AllowInsecure {
			return nil, &ConfigError{Msg: "endpoint uses plain HTTP; set AllowInsecure for local development only"}
		}
	default:
		return nil, &ConfigError{Msg: fmt.Sprintf("unsupported endpoint scheme %q", u.Scheme)}
	}

	root := *u
	root.User = nil // never let a credential embedded in the endpoint leak
	root.RawQuery = ""
	root.Fragment = ""
	if !strings.HasSuffix(root.Path, "/") {
		root.Path += "/"
	}

	base := root
	base.Path += apiPrefix

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	c := &Client{
		baseURL:    &base,
		rootURL:    &root,
		token:      cfg.Token,
		timeout:    timeout,
		httpClient: httpClient,
	}

	if err := c.checkVersion(ctx); err != nil {
		return nil, err
	}

	return c, nil
}

type openAPISchema struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
}

func (c *Client) checkVersion(ctx context.Context) error {
	reqURL := c.rootURL.ResolveReference(&url.URL{Path: schemaPath}).String()
	// Reuses do's transport/auth/timeout plumbing by resolving relative
	// to rootURL directly rather than baseURL — schemaPath sits outside
	// api/0/.
	var schema openAPISchema
	if err := c.doURL(ctx, http.MethodGet, reqURL, nil, &schema); err != nil {
		return &ConfigError{Msg: "fetching OpenAPI schema", Err: err}
	}
	if schema.Info.Version == "" {
		return &ConfigError{Msg: "OpenAPI schema at " + schemaPath + " had no info.version; is this a GlitchTip instance?"}
	}
	return nil
}

// do issues a request against a path relative to the API root and decodes a
// JSON response body into out when out is non-nil.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	reqURL := c.resolve(path)
	_, err := c.request(ctx, method, reqURL, body, out)
	return err
}

// doURL is like do, but reqURL is already absolute (used by checkVersion,
// which requests schemaPath outside the api/0/ prefix do resolves under).
func (c *Client) doURL(ctx context.Context, method, reqURL string, body io.Reader, out any) error {
	_, err := c.request(ctx, method, reqURL, body, out)
	return err
}

// doPage is like do, but for paginated list endpoints: it also returns the
// "next" cursor URL parsed from the response's Link header, or "" when
// there is no further page. path may be a path relative to the API root
// (first page) or an absolute cursor URL previously returned by doPage
// itself (a later page) — resolve leaves absolute URLs untouched.
func (c *Client) doPage(ctx context.Context, method, path string, out any) (string, error) {
	reqURL := c.resolve(path)
	link, err := c.request(ctx, method, reqURL, nil, out)
	if err != nil {
		return "", err
	}
	return parseNextLink(link), nil
}

// resolve turns path into an absolute request URL. A relative path is
// resolved against the API root; an absolute URL (as returned by doPage
// for pagination) is returned unchanged — checkSameHost still rejects it
// in request if it points off-host.
func (c *Client) resolve(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return c.baseURL.ResolveReference(&url.URL{Path: c.baseURL.Path + strings.TrimPrefix(path, "/")}).String()
}

var linkPartRe = regexp.MustCompile(`<([^>]+)>((?:\s*;\s*[a-zA-Z]+="[^"]*")*)`)

// parseNextLink extracts the rel="next" URL from a Sentry/GlitchTip-style
// cursor Link header, returning "" if there is no next page (results="false"
// or no rel="next" segment at all).
func parseNextLink(header string) string {
	if header == "" {
		return ""
	}
	for _, m := range linkPartRe.FindAllStringSubmatch(header, -1) {
		url, params := m[1], m[2]
		if !strings.Contains(params, `rel="next"`) {
			continue
		}
		if strings.Contains(params, `results="false"`) {
			return ""
		}
		return url
	}
	return ""
}

// request is the single low-level entry point every other method funnels
// through. It returns the response's Link header so doPage can parse it;
// do/doURL ignore that return value.
func (c *Client) request(ctx context.Context, method, reqURL string, body io.Reader, out any) (string, error) {
	if err := c.checkSameHost(reqURL); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "terraform-provider-glitchtip-client")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("glitchtip: %s %s: %w", method, reqURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return "", fmt.Errorf("glitchtip: %s %s: reading response: %w", method, reqURL, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &APIError{Method: method, URL: reqURL, StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return "", fmt.Errorf("glitchtip: %s %s: decoding response (status %d, content-type %q): %w",
				method, reqURL, resp.StatusCode, resp.Header.Get("Content-Type"), err)
		}
	}

	return resp.Header.Get("Link"), nil
}

// checkSameHost refuses to follow an absolute pagination URL to a
// different host than the configured endpoint, so a malicious or
// misconfigured server can't redirect the client's bearer token elsewhere.
func (c *Client) checkSameHost(reqURL string) error {
	u, err := url.Parse(reqURL)
	if err != nil {
		return fmt.Errorf("glitchtip: invalid request URL %q: %w", reqURL, err)
	}
	if u.User != nil {
		return &ConfigError{Msg: "refusing to follow a URL that embeds credentials"}
	}
	if !strings.EqualFold(u.Scheme, c.baseURL.Scheme) || !strings.EqualFold(u.Host, c.baseURL.Host) {
		return &ConfigError{Msg: fmt.Sprintf(
			"refusing to follow a URL to a different host (%s://%s), configured endpoint is %s://%s",
			u.Scheme, u.Host, c.baseURL.Scheme, c.baseURL.Host,
		)}
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./src/glitchtip/... -v`
Expected: PASS — all `TestNew_*`, `TestDo_*`, `TestDoPage_*` cases green.

- [ ] **Step 6: Commit**

```bash
git add src/glitchtip/client.go src/glitchtip/errors.go src/glitchtip/client_test.go
git commit -m "feat(client): add GlitchTip HTTP client core

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 4: Client — `src/glitchtip/organizations.go`

**Files:**
- Create: `src/glitchtip/organizations.go`
- Test: `src/glitchtip/organizations_test.go`

**Interfaces:**
- Consumes: `(*Client) do`, `(*Client) doPage`, `*APIError` (Task 3).
- Produces: `Organization{ID, Slug, Name string}`, `(*Client) ListOrganizations(ctx, cursorURL string) (orgs []Organization, nextURL string, err error)`, `CreateOrganizationRequest{Name string}`, `(*Client) CreateOrganization(ctx, in CreateOrganizationRequest) (*Organization, error)`, `(*Client) GetOrganization(ctx, slug string) (*Organization, error)`, `UpdateOrganizationRequest{Name *string}`, `(*Client) UpdateOrganization(ctx, slug string, in UpdateOrganizationRequest) (*Organization, error)`, `(*Client) DeleteOrganization(ctx, slug string) error`. Task 10's `organization_resource.go` calls all of these.

- [ ] **Step 1: Write `organizations_test.go`**

```go
// src/glitchtip/organizations_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newOpenAPIAwareMux(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		handle(w, r)
	}))
}

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := New(context.Background(), Config{Endpoint: srv.URL, Token: "t", AllowInsecure: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return c
}

func TestCreateGetUpdateDeleteOrganization(t *testing.T) {
	orgs := map[string]Organization{}
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/organizations/":
			var in CreateOrganizationRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			org := Organization{ID: "1", Slug: "acme", Name: in.Name}
			orgs["acme"] = org
			_ = json.NewEncoder(w).Encode(org)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/organizations/acme/":
			org, ok := orgs["acme"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(org)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/organizations/acme/":
			var in UpdateOrganizationRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			org := orgs["acme"]
			if in.Name != nil {
				org.Name = *in.Name
			}
			orgs["acme"] = org
			_ = json.NewEncoder(w).Encode(org)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/organizations/acme/":
			delete(orgs, "acme")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	created, err := c.CreateOrganization(ctx, CreateOrganizationRequest{Name: "Acme"})
	if err != nil || created.Slug != "acme" {
		t.Fatalf("create: %+v, err=%v", created, err)
	}

	got, err := c.GetOrganization(ctx, "acme")
	if err != nil || got.Name != "Acme" {
		t.Fatalf("get: %+v, err=%v", got, err)
	}

	newName := "Acme Corp"
	updated, err := c.UpdateOrganization(ctx, "acme", UpdateOrganizationRequest{Name: &newName})
	if err != nil || updated.Name != "Acme Corp" {
		t.Fatalf("update: %+v, err=%v", updated, err)
	}

	if err := c.DeleteOrganization(ctx, "acme"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = c.GetOrganization(ctx, "acme")
	var apiErr *APIError
	if err == nil {
		t.Fatal("expected error after delete")
	}
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}

func TestListOrganizations_Paginates(t *testing.T) {
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			w.Header().Set("Link", `<`+"http://"+r.Host+`/api/0/organizations/?cursor=1>; rel="next"; results="true"; cursor="1"`)
			_ = json.NewEncoder(w).Encode([]Organization{{ID: "1", Slug: "acme", Name: "Acme"}})
			return
		}
		w.Header().Set("Link", `<x>; rel="next"; results="false"; cursor="2"`)
		_ = json.NewEncoder(w).Encode([]Organization{{ID: "2", Slug: "beta", Name: "Beta"}})
	})
	defer srv.Close()
	c := newTestClient(t, srv)

	page1, next1, err := c.ListOrganizations(context.Background(), "")
	if err != nil || len(page1) != 1 || next1 == "" {
		t.Fatalf("page1: %+v next=%q err=%v", page1, next1, err)
	}

	page2, next2, err := c.ListOrganizations(context.Background(), next1)
	if err != nil || len(page2) != 1 || next2 != "" {
		t.Fatalf("page2: %+v next=%q err=%v", page2, next2, err)
	}
}
```

- [ ] **Step 2: Add the `errors.As` helper used above to `errors.go`**

```go
// Appended to src/glitchtip/errors.go
// asAPIError is a small errors.As wrapper kept in this package so client
// tests can assert on *APIError without importing "errors" in every file.
func asAPIError(err error, target **APIError) bool {
	return errorsAs(err, target)
}
```

Add `import "errors"` to `errors.go` and define `var errorsAs = errors.As` right below the imports (a package-level var, not a new indirection layer — this exists purely so `asAPIError`'s body reads naturally; feel free to inline `errors.As` directly instead if that reads cleaner while implementing).

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./src/glitchtip/... -run Organization -v`
Expected: FAIL — `organizations.go` doesn't exist yet.

- [ ] **Step 4: Implement `organizations.go`**

```go
// src/glitchtip/organizations.go
package glitchtip

import (
	"bytes"
	"encoding/json"
	"net/http"

	"context"
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./src/glitchtip/... -run Organization -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src/glitchtip/organizations.go src/glitchtip/organizations_test.go src/glitchtip/errors.go
git commit -m "feat(client): add organizations client

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 5: Client — `src/glitchtip/teams.go`

**Files:**
- Create: `src/glitchtip/teams.go`
- Test: `src/glitchtip/teams_test.go`

**Interfaces:**
- Consumes: `(*Client) do`, `(*Client) doPage` (Task 3); reuses `newOpenAPIAwareMux`/`newTestClient` test helpers (Task 4, same package).
- Produces: `Team{ID, Slug, Name string}`, `(*Client) ListTeams(ctx, orgSlug, cursorURL string) ([]Team, string, error)`, `CreateTeamRequest{Name string}`, `(*Client) CreateTeam(ctx, orgSlug string, in CreateTeamRequest) (*Team, error)`, `(*Client) GetTeam(ctx, orgSlug, teamSlug string) (*Team, error)`, `UpdateTeamRequest{Name *string}`, `(*Client) UpdateTeam(ctx, orgSlug, teamSlug string, in UpdateTeamRequest) (*Team, error)`, `(*Client) DeleteTeam(ctx, orgSlug, teamSlug string) error`. Task 11's `team_resource.go` and Task 6's `projects.go` (project creation is team-scoped) call these.

- [ ] **Step 1: Write `teams_test.go`**

```go
// src/glitchtip/teams_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateGetUpdateDeleteTeam(t *testing.T) {
	teams := map[string]Team{}
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/organizations/acme/teams/":
			var in CreateTeamRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			team := Team{ID: "10", Slug: "platform", Name: in.Name}
			teams["platform"] = team
			_ = json.NewEncoder(w).Encode(team)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/teams/acme/platform/":
			team, ok := teams["platform"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(team)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/teams/acme/platform/":
			var in UpdateTeamRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			team := teams["platform"]
			if in.Name != nil {
				team.Name = *in.Name
			}
			teams["platform"] = team
			_ = json.NewEncoder(w).Encode(team)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/teams/acme/platform/":
			delete(teams, "platform")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	created, err := c.CreateTeam(ctx, "acme", CreateTeamRequest{Name: "Platform"})
	if err != nil || created.Slug != "platform" {
		t.Fatalf("create: %+v, err=%v", created, err)
	}

	got, err := c.GetTeam(ctx, "acme", "platform")
	if err != nil || got.Name != "Platform" {
		t.Fatalf("get: %+v, err=%v", got, err)
	}

	newName := "Platform Engineering"
	updated, err := c.UpdateTeam(ctx, "acme", "platform", UpdateTeamRequest{Name: &newName})
	if err != nil || updated.Name != "Platform Engineering" {
		t.Fatalf("update: %+v, err=%v", updated, err)
	}

	if err := c.DeleteTeam(ctx, "acme", "platform"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = c.GetTeam(ctx, "acme", "platform")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}

func TestListTeams(t *testing.T) {
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/0/organizations/acme/teams/" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]Team{{ID: "10", Slug: "platform", Name: "Platform"}})
	})
	defer srv.Close()
	c := newTestClient(t, srv)

	teams, next, err := c.ListTeams(context.Background(), "acme", "")
	if err != nil || len(teams) != 1 || next != "" {
		t.Fatalf("teams=%+v next=%q err=%v", teams, next, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./src/glitchtip/... -run Team -v`
Expected: FAIL — `teams.go` doesn't exist yet.

- [ ] **Step 3: Implement `teams.go`**

```go
// src/glitchtip/teams.go
package glitchtip

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// Team is a GlitchTip team, scoped to an organization.
type Team struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func teamsListPath(orgSlug string) string  { return "organizations/" + orgSlug + "/teams/" }
func teamPath(orgSlug, teamSlug string) string { return "teams/" + orgSlug + "/" + teamSlug + "/" }

// ListTeams fetches one page of an organization's teams.
func (c *Client) ListTeams(ctx context.Context, orgSlug, cursorURL string) ([]Team, string, error) {
	path := cursorURL
	if path == "" {
		path = teamsListPath(orgSlug)
	}
	var teams []Team
	next, err := c.doPage(ctx, http.MethodGet, path, &teams)
	if err != nil {
		return nil, "", err
	}
	return teams, next, nil
}

// CreateTeamRequest is the writable subset of Team accepted on create.
type CreateTeamRequest struct {
	Name string `json:"name"`
}

// CreateTeam creates a team within orgSlug.
func (c *Client) CreateTeam(ctx context.Context, orgSlug string, in CreateTeamRequest) (*Team, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out Team
	if err := c.do(ctx, http.MethodPost, teamsListPath(orgSlug), bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTeam retrieves a team by organization and team slug.
func (c *Client) GetTeam(ctx context.Context, orgSlug, teamSlug string) (*Team, error) {
	var out Team
	if err := c.do(ctx, http.MethodGet, teamPath(orgSlug, teamSlug), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateTeamRequest is a partial update.
type UpdateTeamRequest struct {
	Name *string `json:"name,omitempty"`
}

// UpdateTeam partially updates a team.
func (c *Client) UpdateTeam(ctx context.Context, orgSlug, teamSlug string, in UpdateTeamRequest) (*Team, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out Team
	if err := c.do(ctx, http.MethodPut, teamPath(orgSlug, teamSlug), bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteTeam deletes a team by organization and team slug.
func (c *Client) DeleteTeam(ctx context.Context, orgSlug, teamSlug string) error {
	return c.do(ctx, http.MethodDelete, teamPath(orgSlug, teamSlug), nil, nil)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./src/glitchtip/... -run Team -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/glitchtip/teams.go src/glitchtip/teams_test.go
git commit -m "feat(client): add teams client

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 6: Client — `src/glitchtip/projects.go`

**Files:**
- Create: `src/glitchtip/projects.go`
- Test: `src/glitchtip/projects_test.go`

**Interfaces:**
- Consumes: `(*Client) do`, `(*Client) doPage` (Task 3); test helpers from Task 4.
- Produces: `Project{ID, Slug, Name, Platform string, EventThrottleRate float64, Teams []TeamRef}`, `TeamRef{Slug string}`, `CreateProjectRequest{Name, Platform string}`, `(*Client) CreateProject(ctx, orgSlug, teamSlug string, in CreateProjectRequest) (*Project, error)`, `(*Client) GetProject(ctx, orgSlug, projectSlug string) (*Project, error)`, `UpdateProjectRequest{Name, Platform *string, EventThrottleRate *float64}`, `(*Client) UpdateProject(ctx, orgSlug, projectSlug string, in UpdateProjectRequest) (*Project, error)`, `(*Client) DeleteProject(ctx, orgSlug, projectSlug string) error`, `(*Client) ListProjects(ctx, orgSlug, cursorURL string) ([]Project, string, error)`. Task 12's `project_resource.go`, Task 7's `project_keys.go`, and Task 8's `project_team_memberships.go` all depend on `Project.Teams` and `GetProject`.

- [ ] **Step 1: Write `projects_test.go`**

```go
// src/glitchtip/projects_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateGetUpdateDeleteProject(t *testing.T) {
	projects := map[string]Project{}
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/teams/acme/platform/projects/":
			var in CreateProjectRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			p := Project{ID: "100", Slug: "checkout", Name: in.Name, Platform: in.Platform,
				Teams: []TeamRef{{Slug: "platform"}}}
			projects["checkout"] = p
			_ = json.NewEncoder(w).Encode(p)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/projects/acme/checkout/":
			p, ok := projects["checkout"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(p)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/projects/acme/checkout/":
			var in UpdateProjectRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			p := projects["checkout"]
			if in.Name != nil {
				p.Name = *in.Name
			}
			if in.EventThrottleRate != nil {
				p.EventThrottleRate = *in.EventThrottleRate
			}
			projects["checkout"] = p
			_ = json.NewEncoder(w).Encode(p)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/projects/acme/checkout/":
			delete(projects, "checkout")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	created, err := c.CreateProject(ctx, "acme", "platform", CreateProjectRequest{Name: "Checkout", Platform: "python"})
	if err != nil || created.Slug != "checkout" || len(created.Teams) != 1 || created.Teams[0].Slug != "platform" {
		t.Fatalf("create: %+v, err=%v", created, err)
	}

	got, err := c.GetProject(ctx, "acme", "checkout")
	if err != nil || got.Name != "Checkout" {
		t.Fatalf("get: %+v, err=%v", got, err)
	}

	rate := 0.5
	updated, err := c.UpdateProject(ctx, "acme", "checkout", UpdateProjectRequest{EventThrottleRate: &rate})
	if err != nil || updated.EventThrottleRate != 0.5 {
		t.Fatalf("update: %+v, err=%v", updated, err)
	}

	if err := c.DeleteProject(ctx, "acme", "checkout"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = c.GetProject(ctx, "acme", "checkout")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}

func TestListProjects(t *testing.T) {
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/0/organizations/acme/projects/" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]Project{{ID: "100", Slug: "checkout", Name: "Checkout"}})
	})
	defer srv.Close()
	c := newTestClient(t, srv)

	projects, next, err := c.ListProjects(context.Background(), "acme", "")
	if err != nil || len(projects) != 1 || next != "" {
		t.Fatalf("projects=%+v next=%q err=%v", projects, next, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./src/glitchtip/... -run Project -v`
Expected: FAIL — `projects.go` doesn't exist yet.

- [ ] **Step 3: Implement `projects.go`**

```go
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

func projectsListPath(orgSlug string) string          { return "organizations/" + orgSlug + "/projects/" }
func projectsCreatePath(orgSlug, teamSlug string) string { return "teams/" + orgSlug + "/" + teamSlug + "/projects/" }
func projectPath(orgSlug, projectSlug string) string   { return "projects/" + orgSlug + "/" + projectSlug + "/" }

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
	Name     string `json:"name"`
	Platform string `json:"platform,omitempty"`
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./src/glitchtip/... -run Project -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/glitchtip/projects.go src/glitchtip/projects_test.go
git commit -m "feat(client): add projects client

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 7: Client — `src/glitchtip/project_keys.go`

**Files:**
- Create: `src/glitchtip/project_keys.go`
- Test: `src/glitchtip/project_keys_test.go`

**Interfaces:**
- Consumes: `(*Client) do` (Task 3); test helpers from Task 4.
- Produces: `ProjectKey{ID, Name string, DSN DSN}`, `DSN{Public, Secret, Security string}`, `CreateProjectKeyRequest{Name string}`, `(*Client) CreateProjectKey(ctx, orgSlug, projectSlug string, in CreateProjectKeyRequest) (*ProjectKey, error)`, `(*Client) GetProjectKey(ctx, orgSlug, projectSlug, keyID string) (*ProjectKey, error)`, `UpdateProjectKeyRequest{Name *string}`, `(*Client) UpdateProjectKey(ctx, orgSlug, projectSlug, keyID string, in UpdateProjectKeyRequest) (*ProjectKey, error)`, `(*Client) DeleteProjectKey(ctx, orgSlug, projectSlug, keyID string) error`. Task 13's `project_key_resource.go` calls all of these.

- [ ] **Step 1: Write `project_keys_test.go`**

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./src/glitchtip/... -run ProjectKey -v`
Expected: FAIL — `project_keys.go` doesn't exist yet.

- [ ] **Step 3: Implement `project_keys.go`**

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./src/glitchtip/... -run ProjectKey -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/glitchtip/project_keys.go src/glitchtip/project_keys_test.go
git commit -m "feat(client): add project keys client

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 8: Client — `src/glitchtip/project_team_memberships.go`

**Files:**
- Create: `src/glitchtip/project_team_memberships.go`
- Test: `src/glitchtip/project_team_memberships_test.go`

**Interfaces:**
- Consumes: `(*Client) do`, `(*Client) GetProject` (Tasks 3, 6).
- Produces: `(*Client) AssignProjectTeam(ctx, orgSlug, projectSlug, teamSlug string) error`, `(*Client) RemoveProjectTeam(ctx, orgSlug, projectSlug, teamSlug string) error`, `(*Client) GetProjectTeamMembership(ctx, orgSlug, projectSlug, teamSlug string) error` (returns nil if the team is currently associated with the project, or a `*APIError` with `NotFound() == true` if not — synthesized client-side from `GetProject`, since there is no single-membership GET endpoint). Task 14's `project_team_membership_resource.go` calls all three.

- [ ] **Step 1: Write `project_team_memberships_test.go`**

```go
// src/glitchtip/project_team_memberships_test.go
package glitchtip

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAssignGetRemoveProjectTeam(t *testing.T) {
	project := Project{ID: "100", Slug: "checkout", Name: "Checkout", Teams: []TeamRef{{Slug: "platform"}}}
	srv := newOpenAPIAwareMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/projects/acme/checkout/teams/sre/":
			project.Teams = append(project.Teams, TeamRef{Slug: "sre"})
			_ = json.NewEncoder(w).Encode(project)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/projects/acme/checkout/teams/sre/":
			kept := project.Teams[:0]
			for _, tm := range project.Teams {
				if tm.Slug != "sre" {
					kept = append(kept, tm)
				}
			}
			project.Teams = kept
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/projects/acme/checkout/":
			_ = json.NewEncoder(w).Encode(project)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	// Not yet assigned.
	err := c.GetProjectTeamMembership(ctx, "acme", "checkout", "sre")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("expected NotFound before assign, got %v", err)
	}

	if err := c.AssignProjectTeam(ctx, "acme", "checkout", "sre"); err != nil {
		t.Fatalf("assign: %v", err)
	}

	if err := c.GetProjectTeamMembership(ctx, "acme", "checkout", "sre"); err != nil {
		t.Fatalf("expected membership to exist after assign, got %v", err)
	}

	if err := c.RemoveProjectTeam(ctx, "acme", "checkout", "sre"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	err = c.GetProjectTeamMembership(ctx, "acme", "checkout", "sre")
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("expected NotFound after remove, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./src/glitchtip/... -run ProjectTeam -v`
Expected: FAIL — `project_team_memberships.go` doesn't exist yet.

- [ ] **Step 3: Implement `project_team_memberships.go`**

```go
// src/glitchtip/project_team_memberships.go
package glitchtip

import (
	"context"
	"net/http"
)

func projectTeamPath(orgSlug, projectSlug, teamSlug string) string {
	return "projects/" + orgSlug + "/" + projectSlug + "/teams/" + teamSlug + "/"
}

// AssignProjectTeam associates teamSlug with projectSlug. Idempotent: the
// API accepts re-assigning a team that's already associated.
func (c *Client) AssignProjectTeam(ctx context.Context, orgSlug, projectSlug, teamSlug string) error {
	return c.do(ctx, http.MethodPost, projectTeamPath(orgSlug, projectSlug, teamSlug), nil, nil)
}

// RemoveProjectTeam disassociates teamSlug from projectSlug.
func (c *Client) RemoveProjectTeam(ctx context.Context, orgSlug, projectSlug, teamSlug string) error {
	return c.do(ctx, http.MethodDelete, projectTeamPath(orgSlug, projectSlug, teamSlug), nil, nil)
}

// GetProjectTeamMembership reports whether teamSlug is currently associated
// with projectSlug. There is no single-membership GET endpoint, so this
// fetches the project and checks its Teams list; when the team isn't
// present it returns a synthesized *APIError with StatusCode 404 (so
// NotFound() behaves the same as every other resource's Read path) rather
// than a bare error, keeping resource Read logic uniform across all five
// resource types.
func (c *Client) GetProjectTeamMembership(ctx context.Context, orgSlug, projectSlug, teamSlug string) error {
	project, err := c.GetProject(ctx, orgSlug, projectSlug)
	if err != nil {
		return err
	}
	for _, tm := range project.Teams {
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./src/glitchtip/... -run ProjectTeam -v`
Expected: PASS.

- [ ] **Step 5: Run the full client test suite**

Run: `go test ./src/glitchtip/... -v`
Expected: PASS — every test from Tasks 3–8 green together.

- [ ] **Step 6: Commit**

```bash
git add src/glitchtip/project_team_memberships.go src/glitchtip/project_team_memberships_test.go
git commit -m "feat(client): add project-team membership client

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 9: Real `provider.go` — schema, Configure, env-var fallback

**Files:**
- Modify: `src/provider/provider.go` (replace the Task 2 placeholder)
- Test: `src/provider/provider_test.go`

**Interfaces:**
- Consumes: `glitchtip.Config`, `glitchtip.New` (Task 3).
- Produces: `provider.New(version string) func() provider.Provider` (unchanged signature from Task 2), a `Configure` that stores `*glitchtip.Client` as both `resp.DataSourceData` and `resp.ResourceData`. Tasks 10–14's resources' `Configure` methods type-assert `req.ProviderData.(*glitchtip.Client)`.

- [ ] **Step 1: Write `provider_test.go`**

```go
// src/provider/provider_test.go
package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestProvider_MetadataAndSchema(t *testing.T) {
	p := New("test")()
	var metaResp providerMetadataResponse
	metaResp.run(t, p)
	if metaResp.TypeName != "glitchtip" {
		t.Fatalf("expected type name glitchtip, got %q", metaResp.TypeName)
	}
}

// TestProvider_ImplementsProtocol6 is a smoke test that the provider server
// can be constructed and speaks protocol 6 without panicking — catches a
// broken Schema()/Configure() wiring before any resource-level test does.
func TestProvider_ImplementsProtocol6(t *testing.T) {
	srv := providerserver.NewProtocol6(New("test")())()
	if srv == nil {
		t.Fatal("expected a non-nil protocol6.ProviderServer")
	}
	var _ tfprotov6.ProviderServer = srv
}
```

`providerMetadataResponse.run` is a tiny local helper — implement it in the
same test file rather than pulling in the framework's full testing harness
for one field:

```go
// Appended to src/provider/provider_test.go
import (
	"github.com/hashicorp/terraform-plugin-framework/provider"
)

type providerMetadataResponse struct {
	TypeName string
}

func (m *providerMetadataResponse) run(t *testing.T, p providerIface) {
	t.Helper()
	resp := &provider.MetadataResponse{}
	p.Metadata(context.Background(), provider.MetadataRequest{}, resp)
	m.TypeName = resp.TypeName
}

type providerIface interface {
	Metadata(context.Context, provider.MetadataRequest, *provider.MetadataResponse)
}
```

(Merge the two `import` blocks in the actual file into one.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./src/provider/... -run Provider -v`
Expected: PASS on `TestProvider_MetadataAndSchema` and `TestProvider_ImplementsProtocol6` even against the Task 2 placeholder (they don't exercise Configure) — so instead confirm the *next* step's behavior compiles by temporarily adding a `TestProvider_ConfigureRequiresToken` stub that references `envAPIToken` — Expected: FAIL to compile, "undefined: envAPIToken", since that constant doesn't exist until Step 3. Add:

```go
// Appended to src/provider/provider_test.go, temporarily, to drive Step 3
func TestProvider_EnvVarConstant(t *testing.T) {
	if envToken != "GLITCHTIP_TOKEN" {
		t.Fatalf("expected GLITCHTIP_TOKEN, got %q", envToken)
	}
}
```

- [ ] **Step 3: Implement `provider.go`**

```go
// src/provider/provider.go
// Package provider implements the GlitchTip Terraform provider.
package provider

import (
	"context"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nmehlei/terraform-provider-glitchtip/src/glitchtip"
)

const (
	envToken = "GLITCHTIP_TOKEN"

	defaultEndpoint = "https://app.glitchtip.com"
)

var _ provider.Provider = &GlitchTipProvider{}

// New returns a provider.Provider factory, as required by
// providerserver.Serve. version is injected at build time (see src/main.go).
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &GlitchTipProvider{version: version}
	}
}

type GlitchTipProvider struct {
	version string
}

type glitchtipProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Token    types.String `tfsdk:"token"`
	Insecure types.Bool   `tfsdk:"insecure"`
}

func (p *GlitchTipProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "glitchtip"
	resp.Version = p.version
}

func (p *GlitchTipProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages resources on a GlitchTip instance's REST API (`/api/0/`).",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "Base URL of the GlitchTip instance. Defaults to `" + defaultEndpoint + "` " +
					"(the hosted SaaS); override for a self-hosted instance, e.g. `https://glitchtip.example.com`.",
				Optional: true,
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "Bearer auth token for the GlitchTip API. Falls back to the `" + envToken +
					"` environment variable when unset; configuration fails if neither is present.",
				Optional:  true,
				Sensitive: true,
			},
			"insecure": schema.BoolAttribute{
				MarkdownDescription: "Allow a plain-HTTP endpoint. Only for local development against a " +
					"disposable instance - never set this against a production endpoint.",
				Optional: true,
			},
		},
	}
}

func (p *GlitchTipProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data glitchtipProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Endpoint.IsUnknown() || data.Token.IsUnknown() || data.Insecure.IsUnknown() {
		// A value that is still unknown at plan time may be supplied by
		// another resource during apply; defer rather than reporting missing.
		return
	}

	endpoint := strings.TrimSpace(data.Endpoint.ValueString())
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	token := strings.TrimSpace(data.Token.ValueString())
	if token == "" {
		token = strings.TrimSpace(os.Getenv(envToken))
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing GlitchTip API Token",
			"Set the token attribute in the provider configuration, or the "+envToken+" environment variable.",
		)
		return
	}

	client, err := glitchtip.New(ctx, glitchtip.Config{
		Endpoint:      endpoint,
		Token:         token,
		AllowInsecure: data.Insecure.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Configure GlitchTip Client", err.Error())
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *GlitchTipProvider) Resources(_ context.Context) []func() resource.Resource {
	// Empty until Tasks 10-14 each add their New*Resource factory here —
	// Task 14's last step is the one that fills this in with all five and
	// re-verifies the full build, so no task in between references a
	// resource file that doesn't exist yet.
	return nil
}

func (p *GlitchTipProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
```

Delete the temporary `TestProvider_EnvVarConstant` stub from Step 2 once
this compiles — its only job was to drive writing `envToken`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./src/provider/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/provider/provider.go src/provider/provider_test.go
git commit -m "feat(provider): implement provider schema and Configure

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---


## Task 10: `glitchtip_organization` resource

**Files:**
- Create: `src/provider/organization_resource.go`
- Modify: `src/provider/provider.go` (add `NewOrganizationResource` to `Resources()`)
- Test: `src/provider/organization_resource_test.go`

**Interfaces:**
- Consumes: `glitchtip.Client`, `glitchtip.Organization`, `CreateOrganizationRequest`, `UpdateOrganizationRequest`, and `(*Client) CreateOrganization/GetOrganization/UpdateOrganization/DeleteOrganization` (Task 4); `*glitchtip.APIError` with `NotFound()` (Task 3).
- Produces: `NewOrganizationResource() resource.Resource`. Referenced by `provider.go` Resources() from this task on.

This is the reference implementation for Tasks 11-14 — the same six methods (Metadata/Schema/Configure/Create/Read/Update/Delete + ImportState + a `modelFromAPI` helper), differing only in fields and client calls. Unlike bugsink's team_resource.go, `Delete` really calls the API.

- [ ] **Step 1: Write `organization_resource_test.go`**

Uses the framework's `resource.Test` acceptance harness driving a real
`terraform` binary against an in-test `httptest` fake of the GlitchTip API.
This is a unit-level check of the resource's plan/apply/refresh/import
wiring; the real-instance lifecycle is Task 16.

```go
// src/provider/organization_resource_test.go
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// fakeGlitchTip is a minimal stateful in-memory GlitchTip API used by every
// resource's *_resource_test.go. Each test registers only the routes it
// needs on top of the always-present /api/openapi.json.
func fakeGlitchTip(t *testing.T, routes http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		routes(w, r)
	}))
}

func protoV6ProviderFactories(endpoint string) map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"glitchtip": func() (tfprotov6.ProviderServer, error) {
			return providerserver.NewProtocol6(New("test")())(), nil
		},
	}
}

func TestAccOrganizationResource_lifecycle(t *testing.T) {
	orgs := map[string]map[string]string{}
	srv := fakeGlitchTip(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/organizations/":
			var in struct{ Name string `json:"name"` }
			_ = json.NewDecoder(r.Body).Decode(&in)
			orgs["acme"] = map[string]string{"id": "1", "slug": "acme", "name": in.Name}
			_ = json.NewEncoder(w).Encode(orgs["acme"])
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/organizations/acme/":
			o, ok := orgs["acme"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(o)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/organizations/acme/":
			var in struct{ Name *string `json:"name"` }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Name != nil {
				orgs["acme"]["name"] = *in.Name
			}
			_ = json.NewEncoder(w).Encode(orgs["acme"])
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/organizations/acme/":
			delete(orgs, "acme")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	t.Setenv("GLITCHTIP_TOKEN", "test-token")

	config := func(name string) string {
		return fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}
resource "glitchtip_organization" "test" {
  name = %q
}
`, srv.URL, name)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(srv.URL),
		Steps: []resource.TestStep{
			{
				Config: config("Acme"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("glitchtip_organization.test", "name", "Acme"),
					resource.TestCheckResourceAttr("glitchtip_organization.test", "slug", "acme"),
					resource.TestCheckResourceAttrSet("glitchtip_organization.test", "id"),
				),
			},
			{
				Config: config("Acme Corp"),
				Check:  resource.TestCheckResourceAttr("glitchtip_organization.test", "name", "Acme Corp"),
			},
			{
				ResourceName:      "glitchtip_organization.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
	_ = context.Background
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TF_ACC=1 go test ./src/provider/... -run TestAccOrganizationResource -v`
Expected: FAIL — `glitchtip_organization` is not a registered resource type yet.

- [ ] **Step 3: Implement `organization_resource.go`**

```go
// src/provider/organization_resource.go
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nmehlei/terraform-provider-glitchtip/src/glitchtip"
)

var (
	_ resource.Resource                = &OrganizationResource{}
	_ resource.ResourceWithImportState = &OrganizationResource{}
)

// NewOrganizationResource is the resource factory registered in provider.go.
func NewOrganizationResource() resource.Resource {
	return &OrganizationResource{}
}

// OrganizationResource manages a GlitchTip organization via full CRUD.
type OrganizationResource struct {
	client *glitchtip.Client
}

type organizationResourceModel struct {
	ID   types.String `tfsdk:"id"`
	Slug types.String `tfsdk:"slug"`
	Name types.String `tfsdk:"name"`
}

func (r *OrganizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *OrganizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A GlitchTip organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Organization ID, assigned by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "URL slug, derived from the name by GlitchTip. Used to address the organization in the API and in `terraform import`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Organization display name.",
				Required:            true,
			},
		},
	}
}

func (r *OrganizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*glitchtip.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("expected *glitchtip.Client, got: %T. Report this to the provider maintainers.", req.ProviderData))
		return
	}
	r.client = client
}

func (r *OrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	org, err := r.client.CreateOrganization(ctx, glitchtip.CreateOrganizationRequest{Name: plan.Name.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating GlitchTip Organization", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, organizationModelFromAPI(org))...)
}

func (r *OrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	org, err := r.client.GetOrganization(ctx, state.Slug.ValueString())
	if err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading GlitchTip Organization", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, organizationModelFromAPI(org))...)
}

func (r *OrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state organizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	update := glitchtip.UpdateOrganizationRequest{}
	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		update.Name = &name
	}

	org, err := r.client.UpdateOrganization(ctx, state.Slug.ValueString(), update)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating GlitchTip Organization", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, organizationModelFromAPI(org))...)
}

func (r *OrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteOrganization(ctx, state.Slug.ValueString()); err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return // already gone
		}
		resp.Diagnostics.AddError("Error Deleting GlitchTip Organization", err.Error())
	}
}

// ImportState accepts the organization slug as the import identifier.
func (r *OrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("slug"), req, resp)
}

func organizationModelFromAPI(org *glitchtip.Organization) organizationResourceModel {
	return organizationResourceModel{
		ID:   types.StringValue(org.ID),
		Slug: types.StringValue(org.Slug),
		Name: types.StringValue(org.Name),
	}
}
```

- [ ] **Step 4: Register in `provider.go`**

Edit `Resources()` in `src/provider/provider.go` to return
`[]func() resource.Resource{NewOrganizationResource}`.

- [ ] **Step 5: Run to verify it passes**

Run: `TF_ACC=1 go test ./src/provider/... -run TestAccOrganizationResource -v`
Expected: PASS — create, update-in-place, and import steps all green.

- [ ] **Step 6: Commit**

```bash
git add src/provider/organization_resource.go src/provider/organization_resource_test.go src/provider/provider.go
git commit -m "feat(provider): add glitchtip_organization resource

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 11: `glitchtip_team` resource + shared compound-import helper

**Files:**
- Create: `src/provider/team_resource.go`
- Create: `src/provider/import_helpers.go`
- Modify: `src/provider/provider.go` (add `NewTeamResource`)
- Test: `src/provider/team_resource_test.go`
- Test: `src/provider/import_helpers_test.go`

**Interfaces:**
- Consumes: `glitchtip.Team`, `Create/Get/Update/DeleteTeam` (Task 5); `fakeGlitchTip`, `protoV6ProviderFactories` test helpers (Task 10).
- Produces: `NewTeamResource() resource.Resource`; `splitImportID(id string, want int) ([]string, error)` in `import_helpers.go` — used by Tasks 12–14 for colon-delimited import identifiers.

- [ ] **Step 1: Write `import_helpers_test.go`**

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./src/provider/... -run TestSplitImportID -v`
Expected: FAIL — `splitImportID` undefined.

- [ ] **Step 3: Implement `import_helpers.go`**

```go
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
```

- [ ] **Step 4: Write `team_resource_test.go`**

```go
// src/provider/team_resource_test.go
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccTeamResource_lifecycle(t *testing.T) {
	teams := map[string]map[string]any{}
	srv := fakeGlitchTip(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/organizations/acme/teams/":
			var in struct{ Name string `json:"name"` }
			_ = json.NewDecoder(r.Body).Decode(&in)
			teams["platform"] = map[string]any{"id": "10", "slug": "platform", "name": in.Name}
			_ = json.NewEncoder(w).Encode(teams["platform"])
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/teams/acme/platform/":
			tm, ok := teams["platform"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(tm)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/teams/acme/platform/":
			var in struct{ Name *string `json:"name"` }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Name != nil {
				teams["platform"]["name"] = *in.Name
			}
			_ = json.NewEncoder(w).Encode(teams["platform"])
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/teams/acme/platform/":
			delete(teams, "platform")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	t.Setenv("GLITCHTIP_TOKEN", "test-token")

	cfg := func(name string) string {
		return fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}
resource "glitchtip_team" "test" {
  organization_slug = "acme"
  name              = %q
}
`, srv.URL, name)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(srv.URL),
		Steps: []resource.TestStep{
			{
				Config: cfg("Platform"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("glitchtip_team.test", "slug", "platform"),
					resource.TestCheckResourceAttr("glitchtip_team.test", "name", "Platform"),
				),
			},
			{Config: cfg("Platform Engineering"), Check: resource.TestCheckResourceAttr("glitchtip_team.test", "name", "Platform Engineering")},
			{
				ResourceName:      "glitchtip_team.test",
				ImportState:       true,
				ImportStateId:     "acme:platform",
				ImportStateVerify: true,
			},
		},
	})
}
```

- [ ] **Step 5: Run to verify it fails**

Run: `TF_ACC=1 go test ./src/provider/... -run TestAccTeamResource -v`
Expected: FAIL — `glitchtip_team` not registered.

- [ ] **Step 6: Implement `team_resource.go`**

```go
// src/provider/team_resource.go
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nmehlei/terraform-provider-glitchtip/src/glitchtip"
)

var (
	_ resource.Resource                = &TeamResource{}
	_ resource.ResourceWithImportState = &TeamResource{}
)

// NewTeamResource is the resource factory registered in provider.go.
func NewTeamResource() resource.Resource { return &TeamResource{} }

// TeamResource manages a GlitchTip team via full CRUD.
type TeamResource struct {
	client *glitchtip.Client
}

type teamResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Slug             types.String `tfsdk:"slug"`
	OrganizationSlug types.String `tfsdk:"organization_slug"`
	Name             types.String `tfsdk:"name"`
}

func (r *TeamResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}

func (r *TeamResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A GlitchTip team within an organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Team ID, assigned by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "URL slug, derived from the name by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the organization this team belongs to. Changing this forces a new team.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Team display name.",
				Required:            true,
			},
		},
	}
}

func (r *TeamResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*glitchtip.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("expected *glitchtip.Client, got: %T.", req.ProviderData))
		return
	}
	r.client = client
}

func (r *TeamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan teamResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	team, err := r.client.CreateTeam(ctx, plan.OrganizationSlug.ValueString(), glitchtip.CreateTeamRequest{Name: plan.Name.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating GlitchTip Team", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, teamModelFromAPI(plan.OrganizationSlug.ValueString(), team))...)
}

func (r *TeamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state teamResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	team, err := r.client.GetTeam(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString())
	if err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading GlitchTip Team", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, teamModelFromAPI(state.OrganizationSlug.ValueString(), team))...)
}

func (r *TeamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state teamResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	update := glitchtip.UpdateTeamRequest{}
	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		update.Name = &name
	}
	team, err := r.client.UpdateTeam(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString(), update)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating GlitchTip Team", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, teamModelFromAPI(state.OrganizationSlug.ValueString(), team))...)
}

func (r *TeamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state teamResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteTeam(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString()); err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return
		}
		resp.Diagnostics.AddError("Error Deleting GlitchTip Team", err.Error())
	}
}

// ImportState expects "<organization_slug>:<team_slug>".
func (r *TeamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 2)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_slug"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), parts[1])...)
}

func teamModelFromAPI(orgSlug string, team *glitchtip.Team) teamResourceModel {
	return teamResourceModel{
		ID:               types.StringValue(team.ID),
		Slug:             types.StringValue(team.Slug),
		OrganizationSlug: types.StringValue(orgSlug),
		Name:             types.StringValue(team.Name),
	}
}
```

- [ ] **Step 7: Register in `provider.go`**

Add `NewTeamResource` to the `Resources()` slice (now
`{NewOrganizationResource, NewTeamResource}`).

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./src/provider/... -run TestSplitImportID -v && TF_ACC=1 go test ./src/provider/... -run TestAccTeamResource -v`
Expected: PASS both.

- [ ] **Step 9: Commit**

```bash
git add src/provider/team_resource.go src/provider/import_helpers.go src/provider/team_resource_test.go src/provider/import_helpers_test.go src/provider/provider.go
git commit -m "feat(provider): add glitchtip_team resource

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 12: `glitchtip_project` resource

**Files:**
- Create: `src/provider/project_resource.go`
- Modify: `src/provider/provider.go` (add `NewProjectResource`)
- Test: `src/provider/project_resource_test.go`

**Interfaces:**
- Consumes: `glitchtip.Project`, `CreateProjectRequest`, `UpdateProjectRequest`, `Create/Get/Update/DeleteProject` (Task 6); `splitImportID` (Task 11); `fakeGlitchTip`/`protoV6ProviderFactories` (Task 10).
- Produces: `NewProjectResource() resource.Resource`.

Notes:
- `initial_team` is create-only (`RequiresReplace`) and is **not** returned by the project GET, so `projectModelFromAPI` takes it as a passed-through argument (same pattern as `teamModelFromAPI`'s `orgSlug`).
- Import identifier is `<organization_slug>:<project_slug>:<initial_team>` — the initial team can't be recovered from the API, so the operator supplies it.
- `platform` and `event_throttle_rate` are `Optional` + `Computed` (GlitchTip assigns defaults); use `UseStateForUnknown` so an unset value doesn't perturb the plan.

- [ ] **Step 1: Write `project_resource_test.go`**

```go
// src/provider/project_resource_test.go
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProjectResource_lifecycle(t *testing.T) {
	proj := map[string]any{}
	srv := fakeGlitchTip(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/teams/acme/platform/projects/":
			var in struct {
				Name     string `json:"name"`
				Platform string `json:"platform"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			proj = map[string]any{"id": "100", "slug": "checkout", "name": in.Name,
				"platform": in.Platform, "eventThrottleRate": 0.0,
				"teams": []map[string]string{{"slug": "platform"}}}
			_ = json.NewEncoder(w).Encode(proj)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/projects/acme/checkout/":
			if len(proj) == 0 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(proj)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/projects/acme/checkout/":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			for k, v := range in {
				proj[k] = v
			}
			_ = json.NewEncoder(w).Encode(proj)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/projects/acme/checkout/":
			proj = map[string]any{}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	t.Setenv("GLITCHTIP_TOKEN", "test-token")

	cfg := func(name string) string {
		return fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}
resource "glitchtip_project" "test" {
  organization_slug = "acme"
  initial_team      = "platform"
  name              = %q
  platform          = "python"
}
`, srv.URL, name)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(srv.URL),
		Steps: []resource.TestStep{
			{
				Config: cfg("Checkout"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("glitchtip_project.test", "slug", "checkout"),
					resource.TestCheckResourceAttr("glitchtip_project.test", "platform", "python"),
				),
			},
			{Config: cfg("Checkout Service"), Check: resource.TestCheckResourceAttr("glitchtip_project.test", "name", "Checkout Service")},
			{
				ResourceName:      "glitchtip_project.test",
				ImportState:       true,
				ImportStateId:     "acme:checkout:platform",
				ImportStateVerify: true,
				// initial_team is set from the import id, not the API, so
				// ImportStateVerify would otherwise flag it.
				ImportStateVerifyIgnore: []string{},
			},
		},
	})
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TF_ACC=1 go test ./src/provider/... -run TestAccProjectResource -v`
Expected: FAIL — `glitchtip_project` not registered.

- [ ] **Step 3: Implement `project_resource.go`**

```go
// src/provider/project_resource.go
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nmehlei/terraform-provider-glitchtip/src/glitchtip"
)

var (
	_ resource.Resource                = &ProjectResource{}
	_ resource.ResourceWithImportState = &ProjectResource{}
)

// NewProjectResource is the resource factory registered in provider.go.
func NewProjectResource() resource.Resource { return &ProjectResource{} }

// ProjectResource manages a GlitchTip project via full CRUD. The project's
// initial team association is set at creation and is immutable; further
// team associations are managed with glitchtip_project_team_membership.
type ProjectResource struct {
	client *glitchtip.Client
}

type projectResourceModel struct {
	ID                types.String  `tfsdk:"id"`
	Slug              types.String  `tfsdk:"slug"`
	OrganizationSlug  types.String  `tfsdk:"organization_slug"`
	InitialTeam       types.String  `tfsdk:"initial_team"`
	Name              types.String  `tfsdk:"name"`
	Platform          types.String  `tfsdk:"platform"`
	EventThrottleRate types.Float64 `tfsdk:"event_throttle_rate"`
}

func (r *ProjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *ProjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A GlitchTip project within an organization. `terraform import` identifier: " +
			"`<organization_slug>:<project_slug>:<initial_team>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Project ID, assigned by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "URL slug, derived from the name by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the organization this project belongs to. Changing this forces a new project.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"initial_team": schema.StringAttribute{
				MarkdownDescription: "Slug of the team the project is created under. GlitchTip requires a project " +
					"to belong to at least one team at creation. Changing this forces a new project; manage " +
					"additional team associations with `glitchtip_project_team_membership`.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Project display name.",
				Required:            true,
			},
			"platform": schema.StringAttribute{
				MarkdownDescription: "Platform identifier (e.g. `python`, `javascript-react`, `go`). Optional; " +
					"GlitchTip leaves it unset if omitted.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"event_throttle_rate": schema.Float64Attribute{
				MarkdownDescription: "Fraction of incoming events to drop, `0.0`–`1.0`. Optional; defaults to the " +
					"GlitchTip server default when omitted.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *ProjectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*glitchtip.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("expected *glitchtip.Client, got: %T.", req.ProviderData))
		return
	}
	r.client = client
}

func (r *ProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := glitchtip.CreateProjectRequest{Name: plan.Name.ValueString()}
	if !plan.Platform.IsNull() && !plan.Platform.IsUnknown() {
		in.Platform = plan.Platform.ValueString()
	}
	project, err := r.client.CreateProject(ctx, plan.OrganizationSlug.ValueString(), plan.InitialTeam.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating GlitchTip Project", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectModelFromAPI(
		plan.OrganizationSlug.ValueString(), plan.InitialTeam.ValueString(), project))...)
}

func (r *ProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, err := r.client.GetProject(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString())
	if err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading GlitchTip Project", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectModelFromAPI(
		state.OrganizationSlug.ValueString(), state.InitialTeam.ValueString(), project))...)
}

func (r *ProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	update := glitchtip.UpdateProjectRequest{}
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		update.Name = &v
	}
	if !plan.Platform.Equal(state.Platform) && !plan.Platform.IsUnknown() {
		v := plan.Platform.ValueString()
		update.Platform = &v
	}
	if !plan.EventThrottleRate.Equal(state.EventThrottleRate) && !plan.EventThrottleRate.IsUnknown() {
		v := plan.EventThrottleRate.ValueFloat64()
		update.EventThrottleRate = &v
	}
	project, err := r.client.UpdateProject(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString(), update)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating GlitchTip Project", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectModelFromAPI(
		state.OrganizationSlug.ValueString(), state.InitialTeam.ValueString(), project))...)
}

func (r *ProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteProject(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString()); err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return
		}
		resp.Diagnostics.AddError("Error Deleting GlitchTip Project", err.Error())
	}
}

// ImportState expects "<organization_slug>:<project_slug>:<initial_team>".
func (r *ProjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 3)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_slug"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("initial_team"), parts[2])...)
}

func projectModelFromAPI(orgSlug, initialTeam string, p *glitchtip.Project) projectResourceModel {
	return projectResourceModel{
		ID:                types.StringValue(p.ID),
		Slug:              types.StringValue(p.Slug),
		OrganizationSlug:  types.StringValue(orgSlug),
		InitialTeam:       types.StringValue(initialTeam),
		Name:              types.StringValue(p.Name),
		Platform:          types.StringValue(p.Platform),
		EventThrottleRate: types.Float64Value(p.EventThrottleRate),
	}
}
```

- [ ] **Step 4: Register in `provider.go`**

Add `NewProjectResource` to `Resources()`.

- [ ] **Step 5: Run to verify it passes**

Run: `TF_ACC=1 go test ./src/provider/... -run TestAccProjectResource -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src/provider/project_resource.go src/provider/project_resource_test.go src/provider/provider.go
git commit -m "feat(provider): add glitchtip_project resource

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 13: `glitchtip_project_key` resource

**Files:**
- Create: `src/provider/project_key_resource.go`
- Modify: `src/provider/provider.go` (add `NewProjectKeyResource`)
- Test: `src/provider/project_key_resource_test.go`

**Interfaces:**
- Consumes: `glitchtip.ProjectKey`, `DSN`, `CreateProjectKeyRequest`, `UpdateProjectKeyRequest`, `Create/Get/Update/DeleteProjectKey` (Task 7); `splitImportID` (Task 11); test helpers (Task 10).
- Produces: `NewProjectKeyResource() resource.Resource`.

Notes:
- `dsn_public` and `dsn_security` are `Computed` + `Sensitive` (Global Constraints).
- `name` is `Optional` + `Computed` (GlitchTip auto-labels keys created without one).
- Import identifier: `<organization_slug>:<project_slug>:<key_id>`.

- [ ] **Step 1: Write `project_key_resource_test.go`**

```go
// src/provider/project_key_resource_test.go
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProjectKeyResource_lifecycle(t *testing.T) {
	base := "/api/0/projects/acme/checkout/keys/"
	key := map[string]any{}
	srv := fakeGlitchTip(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			var in struct{ Name string `json:"name"` }
			_ = json.NewDecoder(r.Body).Decode(&in)
			key = map[string]any{"id": "key1", "label": in.Name, "dsn": map[string]string{
				"public": "https://pub@acme.example/100", "security": "https://acme.example/api/100/security/?glitchtip_key=pub"}}
			_ = json.NewEncoder(w).Encode(key)
		case r.Method == http.MethodGet && r.URL.Path == base+"key1/":
			if len(key) == 0 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(key)
		case r.Method == http.MethodPut && r.URL.Path == base+"key1/":
			var in struct{ Name *string `json:"name"` }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Name != nil {
				key["label"] = *in.Name
			}
			_ = json.NewEncoder(w).Encode(key)
		case r.Method == http.MethodDelete && r.URL.Path == base+"key1/":
			key = map[string]any{}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	t.Setenv("GLITCHTIP_TOKEN", "test-token")

	cfg := func(name string) string {
		return fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}
resource "glitchtip_project_key" "test" {
  organization_slug = "acme"
  project_slug      = "checkout"
  name              = %q
}
`, srv.URL, name)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(srv.URL),
		Steps: []resource.TestStep{
			{
				Config: cfg("default"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("glitchtip_project_key.test", "name", "default"),
					resource.TestCheckResourceAttrSet("glitchtip_project_key.test", "dsn_public"),
				),
			},
			{Config: cfg("renamed"), Check: resource.TestCheckResourceAttr("glitchtip_project_key.test", "name", "renamed")},
			{
				ResourceName:      "glitchtip_project_key.test",
				ImportState:       true,
				ImportStateId:     "acme:checkout:key1",
				ImportStateVerify: true,
			},
		},
	})
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TF_ACC=1 go test ./src/provider/... -run TestAccProjectKeyResource -v`
Expected: FAIL — `glitchtip_project_key` not registered.

- [ ] **Step 3: Implement `project_key_resource.go`**

```go
// src/provider/project_key_resource.go
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nmehlei/terraform-provider-glitchtip/src/glitchtip"
)

var (
	_ resource.Resource                = &ProjectKeyResource{}
	_ resource.ResourceWithImportState = &ProjectKeyResource{}
)

// NewProjectKeyResource is the resource factory registered in provider.go.
func NewProjectKeyResource() resource.Resource { return &ProjectKeyResource{} }

// ProjectKeyResource manages a GlitchTip project key (DSN) via full CRUD.
type ProjectKeyResource struct {
	client *glitchtip.Client
}

type projectKeyResourceModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationSlug types.String `tfsdk:"organization_slug"`
	ProjectSlug      types.String `tfsdk:"project_slug"`
	Name             types.String `tfsdk:"name"`
	DSNPublic        types.String `tfsdk:"dsn_public"`
	DSNSecurity      types.String `tfsdk:"dsn_security"`
}

func (r *ProjectKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_key"
}

func (r *ProjectKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A GlitchTip project key. Exposes the DSN clients send events with. " +
			"`terraform import` identifier: `<organization_slug>:<project_slug>:<key_id>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Key ID, assigned by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the organization the project belongs to. Changing this forces a new key.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"project_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the project this key belongs to. Changing this forces a new key.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Key label. Optional; GlitchTip assigns a default label when omitted.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dsn_public": schema.StringAttribute{
				MarkdownDescription: "Public DSN clients use to send events. Sensitive.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dsn_security": schema.StringAttribute{
				MarkdownDescription: "Security-header reporting endpoint DSN. Sensitive.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *ProjectKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*glitchtip.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("expected *glitchtip.Client, got: %T.", req.ProviderData))
		return
	}
	r.client = client
}

func (r *ProjectKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.client.CreateProjectKey(ctx, plan.OrganizationSlug.ValueString(), plan.ProjectSlug.ValueString(),
		glitchtip.CreateProjectKeyRequest{Name: plan.Name.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating GlitchTip Project Key", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectKeyModelFromAPI(
		plan.OrganizationSlug.ValueString(), plan.ProjectSlug.ValueString(), key))...)
}

func (r *ProjectKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.client.GetProjectKey(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), state.ID.ValueString())
	if err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading GlitchTip Project Key", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectKeyModelFromAPI(
		state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), key))...)
}

func (r *ProjectKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	update := glitchtip.UpdateProjectKeyRequest{}
	if !plan.Name.Equal(state.Name) && !plan.Name.IsUnknown() {
		v := plan.Name.ValueString()
		update.Name = &v
	}
	key, err := r.client.UpdateProjectKey(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), state.ID.ValueString(), update)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating GlitchTip Project Key", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectKeyModelFromAPI(
		state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), key))...)
}

func (r *ProjectKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteProjectKey(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), state.ID.ValueString()); err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return
		}
		resp.Diagnostics.AddError("Error Deleting GlitchTip Project Key", err.Error())
	}
}

// ImportState expects "<organization_slug>:<project_slug>:<key_id>".
func (r *ProjectKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 3)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_slug"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_slug"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[2])...)
}

func projectKeyModelFromAPI(orgSlug, projectSlug string, k *glitchtip.ProjectKey) projectKeyResourceModel {
	return projectKeyResourceModel{
		ID:               types.StringValue(k.ID),
		OrganizationSlug: types.StringValue(orgSlug),
		ProjectSlug:      types.StringValue(projectSlug),
		Name:             types.StringValue(k.Name),
		DSNPublic:        types.StringValue(k.DSN.Public),
		DSNSecurity:      types.StringValue(k.DSN.Security),
	}
}
```

- [ ] **Step 4: Register in `provider.go`**

Add `NewProjectKeyResource` to `Resources()`.

- [ ] **Step 5: Run to verify it passes**

Run: `TF_ACC=1 go test ./src/provider/... -run TestAccProjectKeyResource -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src/provider/project_key_resource.go src/provider/project_key_resource_test.go src/provider/provider.go
git commit -m "feat(provider): add glitchtip_project_key resource

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 14: `glitchtip_project_team_membership` resource + finalize `Resources()`

**Files:**
- Create: `src/provider/project_team_membership_resource.go`
- Modify: `src/provider/provider.go` (add the fifth factory; final `Resources()` shape)
- Test: `src/provider/project_team_membership_resource_test.go`

**Interfaces:**
- Consumes: `Assign/Remove/GetProjectTeamMembership` (Task 8); `splitImportID` (Task 11); test helpers (Task 10).
- Produces: `NewProjectTeamMembershipResource() resource.Resource`.

Notes:
- Every attribute is `RequiresReplace` — this resource is a bare link with no mutable field, so `Update` is unreachable in practice but still implemented (it copies plan→state) to satisfy the interface.
- Synthetic `id` = `<organization_slug>:<project_slug>:<team_slug>`, which is also the import identifier.

- [ ] **Step 1: Write `project_team_membership_resource_test.go`**

```go
// src/provider/project_team_membership_resource_test.go
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProjectTeamMembershipResource_lifecycle(t *testing.T) {
	teams := []map[string]string{{"slug": "platform"}}
	srv := fakeGlitchTip(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/projects/acme/checkout/teams/sre/":
			teams = append(teams, map[string]string{"slug": "sre"})
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": "checkout", "teams": teams})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/projects/acme/checkout/teams/sre/":
			kept := teams[:0]
			for _, tm := range teams {
				if tm["slug"] != "sre" {
					kept = append(kept, tm)
				}
			}
			teams = kept
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/projects/acme/checkout/":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "100", "slug": "checkout", "name": "Checkout", "teams": teams})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	t.Setenv("GLITCHTIP_TOKEN", "test-token")

	cfg := fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}
resource "glitchtip_project_team_membership" "test" {
  organization_slug = "acme"
  project_slug      = "checkout"
  team_slug         = "sre"
}
`, srv.URL)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(srv.URL),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.TestCheckResourceAttr(
					"glitchtip_project_team_membership.test", "id", "acme:checkout:sre"),
			},
			{
				ResourceName:      "glitchtip_project_team_membership.test",
				ImportState:       true,
				ImportStateId:     "acme:checkout:sre",
				ImportStateVerify: true,
			},
		},
	})
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TF_ACC=1 go test ./src/provider/... -run TestAccProjectTeamMembership -v`
Expected: FAIL — resource type not registered.

- [ ] **Step 3: Implement `project_team_membership_resource.go`**

```go
// src/provider/project_team_membership_resource.go
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nmehlei/terraform-provider-glitchtip/src/glitchtip"
)

var (
	_ resource.Resource                = &ProjectTeamMembershipResource{}
	_ resource.ResourceWithImportState = &ProjectTeamMembershipResource{}
)

// NewProjectTeamMembershipResource is the resource factory registered in provider.go.
func NewProjectTeamMembershipResource() resource.Resource { return &ProjectTeamMembershipResource{} }

// ProjectTeamMembershipResource associates one existing team with one
// existing project, beyond the project's initial_team. Removing it from
// configuration detaches that team; it never touches the initial team.
type ProjectTeamMembershipResource struct {
	client *glitchtip.Client
}

type projectTeamMembershipResourceModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationSlug types.String `tfsdk:"organization_slug"`
	ProjectSlug      types.String `tfsdk:"project_slug"`
	TeamSlug         types.String `tfsdk:"team_slug"`
}

func (r *ProjectTeamMembershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_team_membership"
}

func (r *ProjectTeamMembershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Associates an existing team with an existing project. Manage the project's first " +
			"team with `glitchtip_project`'s `initial_team`; use this resource for every additional team. " +
			"`terraform import` identifier: `<organization_slug>:<project_slug>:<team_slug>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Synthetic identifier: `<organization_slug>:<project_slug>:<team_slug>`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the organization. Changing this forces a new association.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"project_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the project. Changing this forces a new association.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"team_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the team to associate. Changing this forces a new association.",
				Required:            true,
				PlanModifiers:       replace,
			},
		},
	}
}

func (r *ProjectTeamMembershipResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*glitchtip.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("expected *glitchtip.Client, got: %T.", req.ProviderData))
		return
	}
	r.client = client
}

func (m projectTeamMembershipResourceModel) syntheticID() string {
	return m.OrganizationSlug.ValueString() + ":" + m.ProjectSlug.ValueString() + ":" + m.TeamSlug.ValueString()
}

func (r *ProjectTeamMembershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectTeamMembershipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.AssignProjectTeam(ctx, plan.OrganizationSlug.ValueString(), plan.ProjectSlug.ValueString(), plan.TeamSlug.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Associating Team With Project", err.Error())
		return
	}
	plan.ID = types.StringValue(plan.syntheticID())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ProjectTeamMembershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectTeamMembershipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.GetProjectTeamMembership(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), state.TeamSlug.ValueString())
	if err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Project-Team Association", err.Error())
		return
	}
	state.ID = types.StringValue(state.syntheticID())
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is unreachable in practice (every attribute is RequiresReplace) but
// implemented to satisfy the resource.Resource interface.
func (r *ProjectTeamMembershipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectTeamMembershipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(plan.syntheticID())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ProjectTeamMembershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectTeamMembershipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.RemoveProjectTeam(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), state.TeamSlug.ValueString()); err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return
		}
		resp.Diagnostics.AddError("Error Removing Project-Team Association", err.Error())
	}
}

// ImportState expects "<organization_slug>:<project_slug>:<team_slug>".
func (r *ProjectTeamMembershipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 3)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_slug"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_slug"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("team_slug"), parts[2])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
```

- [ ] **Step 4: Finalize `provider.go` `Resources()`**

Set the slice to all five, in this order:

```go
func (p *GlitchTipProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewOrganizationResource,
		NewTeamResource,
		NewProjectResource,
		NewProjectKeyResource,
		NewProjectTeamMembershipResource,
	}
}
```

- [ ] **Step 5: Run the full provider + client suite and vet**

Run: `gofmt -l src/ && go vet ./src/... && go build ./src/... && TF_ACC=1 go test ./src/... -v`
Expected: no gofmt output, vet clean, build ok, every `TestAcc*` and client test green.

- [ ] **Step 6: Commit**

```bash
git add src/provider/project_team_membership_resource.go src/provider/project_team_membership_resource_test.go src/provider/provider.go
git commit -m "feat(provider): add glitchtip_project_team_membership resource

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 15: Examples and generated Registry docs

**Files:**
- Create: `examples/provider/provider.tf`
- Create: `examples/resources/glitchtip_organization/{resource.tf,import.sh}`
- Create: `examples/resources/glitchtip_team/{resource.tf,import.sh}`
- Create: `examples/resources/glitchtip_project/{resource.tf,import.sh}`
- Create: `examples/resources/glitchtip_project_key/{resource.tf,import.sh}`
- Create: `examples/resources/glitchtip_project_team_membership/{resource.tf,import.sh}`
- Create: `docs/**` (generated — do not hand-edit)
- Test: `src/provider/docs_test.go`

**Interfaces:**
- Consumes: all five registered resources (Tasks 10–14).
- Produces: `docs/index.md` + `docs/resources/*.md` generated by `tfplugindocs`, verified in sync by a test.

- [ ] **Step 1: Write the example files**

`examples/provider/provider.tf`:

```hcl
terraform {
  required_providers {
    glitchtip = {
      source = "nmehlei/glitchtip"
    }
  }
}

provider "glitchtip" {
  endpoint = "https://app.glitchtip.com"
  # token is sensitive - prefer the GLITCHTIP_TOKEN environment variable.
}
```

`examples/resources/glitchtip_organization/resource.tf`:

```hcl
resource "glitchtip_organization" "acme" {
  name = "Acme"
}
```

`examples/resources/glitchtip_organization/import.sh`:

```shell
terraform import glitchtip_organization.acme acme
```

`examples/resources/glitchtip_team/resource.tf`:

```hcl
resource "glitchtip_team" "platform" {
  organization_slug = glitchtip_organization.acme.slug
  name              = "Platform"
}
```

`examples/resources/glitchtip_team/import.sh`:

```shell
terraform import glitchtip_team.platform acme:platform
```

`examples/resources/glitchtip_project/resource.tf`:

```hcl
resource "glitchtip_project" "checkout" {
  organization_slug   = glitchtip_organization.acme.slug
  initial_team        = glitchtip_team.platform.slug
  name                = "checkout-service"
  platform            = "python"
  event_throttle_rate = 0.0
}
```

`examples/resources/glitchtip_project/import.sh`:

```shell
terraform import glitchtip_project.checkout acme:checkout-service:platform
```

`examples/resources/glitchtip_project_key/resource.tf`:

```hcl
resource "glitchtip_project_key" "default" {
  organization_slug = glitchtip_organization.acme.slug
  project_slug      = glitchtip_project.checkout.slug
  name              = "default"
}

output "checkout_dsn" {
  value     = glitchtip_project_key.default.dsn_public
  sensitive = true
}
```

`examples/resources/glitchtip_project_key/import.sh`:

```shell
terraform import glitchtip_project_key.default acme:checkout-service:1
```

`examples/resources/glitchtip_project_team_membership/resource.tf`:

```hcl
resource "glitchtip_team" "sre" {
  organization_slug = glitchtip_organization.acme.slug
  name              = "SRE"
}

resource "glitchtip_project_team_membership" "checkout_sre" {
  organization_slug = glitchtip_organization.acme.slug
  project_slug      = glitchtip_project.checkout.slug
  team_slug         = glitchtip_team.sre.slug
}
```

`examples/resources/glitchtip_project_team_membership/import.sh`:

```shell
terraform import glitchtip_project_team_membership.checkout_sre acme:checkout-service:sre
```

- [ ] **Step 2: Generate the docs**

Run: `cd src && go generate ./... && cd ..`
Expected: creates `docs/index.md` and `docs/resources/{organization,team,project,project_key,project_team_membership}.md`.

- [ ] **Step 3: Write `docs_test.go` — fail if docs are stale**

```go
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
```

- [ ] **Step 4: Run it**

Run: `go test ./src/provider/... -run TestDocsAreGenerated -v`
Expected: PASS (docs just generated and committed match).

- [ ] **Step 5: Commit**

```bash
git add examples/ docs/ src/provider/docs_test.go
git commit -m "docs: add examples and generated Registry documentation

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 16: Real-instance acceptance tests

**Files:**
- Create: `tests/acceptance/acceptance_test.go`
- Create: `tests/acceptance/README.md`
- Modify: `tests/acceptance/up.sh` (also mint and print an auth token via the GlitchTip management shell)

**Interfaces:**
- Consumes: the running Docker GlitchTip from Task 1's `docker-compose.yml`; the built provider binary via a `dev_overrides`-style `TF_ACC` in-process factory.
- Produces: one `TestAccGlitchTipLifecycle` that drives all five resources through create → update → import → destroy against the real API, gated on `GLITCHTIP_ACCEPTANCE=1`.

- [ ] **Step 1: Extend `up.sh` to emit a token**

Append to `tests/acceptance/up.sh`, after the health check:

```bash
# Create a superuser and an org-scoped auth token non-interactively.
docker compose exec -T web ./manage.py shell -c "
from django.contrib.auth import get_user_model
from organizations_ext.models import Organization
from api_tokens.models import APIToken
U = get_user_model()
u, _ = U.objects.get_or_create(email='acc@example.com', defaults={'is_superuser': True, 'is_staff': True})
u.set_password('acceptance'); u.save()
org, _ = Organization.objects.get_or_create(name='Acceptance', slug='acceptance')
org.add_user(u)
tok, _ = APIToken.objects.get_or_create(user=u, label='acceptance')
print('GLITCHTIP_TOKEN=' + tok.token)
print('GLITCHTIP_ORG=' + org.slug)
"
```

Note: the model import paths above (`organizations_ext.models`, `api_tokens.models`) are the current GlitchTip module names as of writing — confirm them against the running image in Task 1's exploration and adjust here. If a management shell approach proves brittle, fall back to registering the first user via the public API (`POST /api/0/auth/register/` or the UI) as Task 1 already documents, and reading the token the UI issues.

- [ ] **Step 2: Write `acceptance_test.go`**

```go
// tests/acceptance/acceptance_test.go
package acceptance

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/nmehlei/terraform-provider-glitchtip/src/provider"
)

// gate skips the whole file unless GLITCHTIP_ACCEPTANCE=1 and the endpoint +
// token are present, so `go test ./...` stays runnable without Docker.
func gate(t *testing.T) (endpoint, token string) {
	t.Helper()
	if os.Getenv("GLITCHTIP_ACCEPTANCE") != "1" {
		t.Skip("set GLITCHTIP_ACCEPTANCE=1 (and run tests/acceptance/up.sh) to run real-instance acceptance tests")
	}
	endpoint = os.Getenv("GLITCHTIP_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8000"
	}
	token = os.Getenv("GLITCHTIP_TOKEN")
	if token == "" {
		t.Fatal("GLITCHTIP_TOKEN is required when GLITCHTIP_ACCEPTANCE=1")
	}
	return endpoint, token
}

func factories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"glitchtip": func() (tfprotov6.ProviderServer, error) {
			return providerserver.NewProtocol6(provider.New("acc")())(), nil
		},
	}
}

func TestAccGlitchTipLifecycle(t *testing.T) {
	endpoint, token := gate(t)
	t.Setenv("GLITCHTIP_TOKEN", token)

	// Unique suffix so repeated runs against a persistent instance don't collide.
	suffix := fmt.Sprintf("tf-acc-%d", os.Getpid())

	base := fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}

resource "glitchtip_organization" "o" {
  name = "%s-org"
}

resource "glitchtip_team" "t" {
  organization_slug = glitchtip_organization.o.slug
  name              = "%s-team"
}

resource "glitchtip_team" "t2" {
  organization_slug = glitchtip_organization.o.slug
  name              = "%s-team2"
}

resource "glitchtip_project" "p" {
  organization_slug = glitchtip_organization.o.slug
  initial_team      = glitchtip_team.t.slug
  name              = "%s-proj"
  platform          = "python"
}

resource "glitchtip_project_key" "k" {
  organization_slug = glitchtip_organization.o.slug
  project_slug      = glitchtip_project.p.slug
  name              = "default"
}

resource "glitchtip_project_team_membership" "m" {
  organization_slug = glitchtip_organization.o.slug
  project_slug      = glitchtip_project.p.slug
  team_slug         = glitchtip_team.t2.slug
}
`, endpoint, suffix, suffix, suffix, suffix)

	updated := base // second step changes the project name
	updated = fmt.Sprintf("%s\n# bump\n", updated)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(),
		Steps: []resource.TestStep{
			{
				Config: base,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("glitchtip_project_key.k", "dsn_public"),
					resource.TestCheckResourceAttr("glitchtip_project_team_membership.m", "team_slug", suffix+"-team2"),
				),
			},
			{
				ResourceName:      "glitchtip_project_key.k",
				ImportState:       true,
				ImportStateIdFunc: func(s *resource.State) (string, error) {
					rs := s.RootModule().Resources["glitchtip_project_key.k"]
					return fmt.Sprintf("%s:%s:%s",
						rs.Primary.Attributes["organization_slug"],
						rs.Primary.Attributes["project_slug"],
						rs.Primary.Attributes["id"]), nil
				},
				ImportStateVerify: true,
			},
			{Config: updated}, // re-apply, expect empty plan (drift check)
		},
	})
}
```

- [ ] **Step 3: Write `tests/acceptance/README.md`**

```markdown
# Acceptance tests

These run against a **real, disposable** GlitchTip instance in Docker.
They create and destroy real organizations, teams, projects and keys.
Never point them at a shared or production instance.

```shell
./up.sh                       # starts GlitchTip, prints GLITCHTIP_TOKEN / GLITCHTIP_ORG
export GLITCHTIP_ACCEPTANCE=1
export GLITCHTIP_ENDPOINT=http://localhost:8000
export GLITCHTIP_TOKEN=<from up.sh>
go test ./tests/acceptance/... -v -timeout 20m
./down.sh                     # tears everything down, removes volumes
```

Without `GLITCHTIP_ACCEPTANCE=1` the suite skips, so `go test ./...` stays
green with no Docker.
```

- [ ] **Step 4: Run against a live instance once, locally**

Run:
```
tests/acceptance/up.sh
export GLITCHTIP_ACCEPTANCE=1 GLITCHTIP_ENDPOINT=http://localhost:8000
export GLITCHTIP_TOKEN=<printed by up.sh>
go test ./tests/acceptance/... -v -timeout 20m
tests/acceptance/down.sh
```
Expected: PASS — all five resources created, key DSN populated, import verified, re-apply produces an empty plan, destroy removes everything. **If field names or paths differ from Tasks 4–8, fix them in `src/glitchtip/` now and re-run the client unit tests** — this is the verification step the design doc's "confirm against a running instance" note points at.

- [ ] **Step 5: Commit**

```bash
git add tests/acceptance/
git commit -m "test: add real-instance acceptance suite

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 17: CI workflows

**Files:**
- Create: `.github/workflows/test.yml`
- Create: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: everything above.
- Produces: PR-time verification (unit + real-instance acceptance) and push-to-`main` signed releases.

- [ ] **Step 1: `.github/workflows/test.yml`**

```yaml
name: test

on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  unit:
    name: unit tests
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: gofmt -l src/
      - run: go vet ./src/...
      - run: go build ./src/...
      - run: go test -race ./src/... -timeout 120s

  acceptance:
    name: acceptance tests (disposable GlitchTip)
    runs-on: ubuntu-latest
    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: hashicorp/setup-terraform@v3
        with:
          terraform_wrapper: false
      - name: Start disposable GlitchTip
        working-directory: tests/acceptance
        run: ./up.sh | tee up.out
      - name: Run acceptance tests
        working-directory: .
        env:
          GLITCHTIP_ACCEPTANCE: "1"
          GLITCHTIP_ENDPOINT: http://localhost:8000
        run: |
          export GLITCHTIP_TOKEN="$(grep -m1 '^GLITCHTIP_TOKEN=' tests/acceptance/up.out | cut -d= -f2-)"
          test -n "$GLITCHTIP_TOKEN"
          go test ./tests/acceptance/... -v -timeout 20m
      - name: Tear down
        if: always()
        working-directory: tests/acceptance
        run: ./down.sh
```

- [ ] **Step 2: `.github/workflows/release.yml`**

Copy `terraform-provider-bugsink`'s `release.yml` verbatim except the two
provider-name references — it already documents (in its header comment) why
tag + release live in one job. The relevant body:

```yaml
name: release

on:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Install GitVersion
        uses: gittools/actions/gitversion/setup@v3
        with:
          versionSpec: "5.x"
      - name: Determine version
        id: gitversion
        uses: gittools/actions/gitversion/execute@v3
      - name: Tag this commit
        id: tag
        env:
          VERSION: v${{ steps.gitversion.outputs.semVer }}
        run: |
          if git rev-parse "$VERSION" >/dev/null 2>&1; then
            echo "Tag $VERSION already exists, skipping."
            echo "skip=true" >> "$GITHUB_OUTPUT"
            exit 0
          fi
          git config user.name "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
          git tag -a "$VERSION" -m "$VERSION"
          git push origin "$VERSION"
          echo "skip=false" >> "$GITHUB_OUTPUT"
      - name: Import GPG key
        if: steps.tag.outputs.skip != 'true'
        uses: crazy-max/ghaction-import-gpg@v6
        id: import_gpg
        with:
          gpg_private_key: ${{ secrets.GPG_PRIVATE_KEY }}
          passphrase: ${{ secrets.GPG_PASSPHRASE }}
      - uses: goreleaser/goreleaser-action@v6
        if: steps.tag.outputs.skip != 'true'
        with:
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GPG_FINGERPRINT: ${{ steps.import_gpg.outputs.fingerprint }}
```

- [ ] **Step 3: Validate workflow YAML locally**

Run: `python3 -c "import yaml,sys; [yaml.safe_load(open(f)) for f in sys.argv[1:]]" .github/workflows/test.yml .github/workflows/release.yml`
Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/
git commit -m "ci: add test and release workflows

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Task 18: README

**Files:**
- Create: `README.md`

**Interfaces:**
- Consumes: everything. Produces the Registry landing content.

- [ ] **Step 1: Write `README.md`**

Follow `terraform-provider-bugsink`'s README structure: title + one-line
description; a "What's supported" table (all five resources: List/Get,
Create, Update, Delete = yes for every row); a `provider` + resource
`hcl` quick-start block mirroring `examples/`; a "Configuration" table
(`endpoint` default `https://app.glitchtip.com`, `token` /
`GLITCHTIP_TOKEN` sensitive, `insecure` local-dev only); an "Import"
section pointing at each resource's docs page for its exact identifier
format; a "Testing" section describing `go test ./src/...` (unit) and
`tests/acceptance` (real Docker instance, `GLITCHTIP_ACCEPTANCE=1`); and
a "License" section (Apache-2.0). No "delete fails by design" caveat —
GlitchTip supports delete for every resource.

- [ ] **Step 2: Check links and headings**

Run: `grep -n '](' README.md`
Expected: every relative link (`docs/...`, `examples/...`, `LICENSE`)
points at a path that exists — verify each with `ls`.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add README

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Self-review notes

- **Spec coverage:** provider config (Task 9), all five resources with full CRUD + import + drift (Tasks 10–14), client with credential-scrubbing / host-locked pagination / version check (Task 3), Sentry-style Link-header pagination (Task 3), DSN-as-sensitive (Task 13), real-instance testing / no mock (Tasks 1, 16), GitVersion + GoReleaser signed Registry releases (Tasks 2, 17), generated docs (Task 15), `src/` + `tests/` layout (throughout). The spec's "data sources: none in v1" is honored by `DataSources()` returning `nil` (Task 9).
- **Retry on 429/5xx** (spec's client section) is described in the spec as carried over from revenuecat but is **not** implemented in Task 3's code above — the client has no retry loop. Decision for the executor: this is acceptable for v1 against a self-hosted instance and a first Registry release; if hosted-SaaS rate limiting shows up in Task 16, add a bounded retry in `request` (wrap the `c.httpClient.Do` call) as a follow-up task before publishing, and add a `max_retries` provider attribute. Not a blocker for the plan.
- **`platform` / `event_throttle_rate` field names** (Task 6) and the **auth-token minting path** (Task 16 Step 1) are the two spots flagged to confirm against a live instance in Task 1 / Task 16 Step 4. The code commits to concrete names; the plan says explicitly where to correct them.
- **Type consistency:** `modelFromAPI` helpers all take passthrough args for fields absent from the API response (`orgSlug`, `initialTeam`, `projectSlug`); client `List*` methods all return `([]T, string, error)`; every resource `Read` uses `errors.As(&glitchtip.APIError)` + `NotFound()`. Consistent across Tasks 4–14.
