// Command terraform-provider-observe is the Terraform provider for Observe (observe.live).
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/shaferllc/terraform-provider-observe/internal/provider"
)

// version is set by GoReleaser.
var version = "dev"

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name observe

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers such as delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		// The registry address; change the namespace with the GitHub organization the provider is published from.
		Address: "registry.terraform.io/shaferllc/observe",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
