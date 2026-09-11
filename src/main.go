// src/main.go
package main

// tfplugindocs: --provider-dir "." points to src/ itself (not ".."/repo root) because
// tfplugindocs runs a bare "go build" in --provider-dir with no package pattern,
// requiring main.go directly in that directory. --examples-dir and --rendered-website-dir
// repoint outputs back to the repo root since they default to --provider-dir-relative.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-dir . --provider-name glitchtip --examples-dir ../examples --rendered-website-dir ../docs

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
