package main

import (
	"flag"

	"github.com/dmacvicar/terraform-provider-libvirt/libvirt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
)

var (
	flagDebug = flag.Bool("debug", false, "enable debugger support")
	flagProiverAddr = flag.String("addr", "registry.terraform.io/cyril-s/libvirt", "full proivder address as written in the terraform configuration")
)

func main() {
	defer libvirt.CleanupLibvirtConnections()

	flag.Parse()

	plugin.Serve(&plugin.ServeOpts{
		Debug: *flagDebug,
		ProviderAddr: *flagProiverAddr,
		ProviderFunc: libvirt.Provider,
	})
}
