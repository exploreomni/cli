package main

import (
	"testing"

	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Every persistent flag on the root is inherited by every generated command,
// so the command generator has to know not to hand its name to a spec query
// param — otherwise the param's flag wins the slot (pflag's AddFlagSet skips a
// persistent flag whose name is already taken) and, for --base-url or --token,
// a query value would end up steering the request itself.
//
// This test is the drift guard: add a global flag without adding it to
// reservedFlagKeys in internal/openapi/generate.go and it fails here.
func TestGlobalFlagsAreReserved(t *testing.T) {
	root := &cobra.Command{Use: "omni"}
	addGlobalFlags(root)

	seen := 0
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		seen++
		if !openapi.IsReservedFlagName(f.Name) {
			t.Errorf("global flag --%s is not in openapi's reserved flag names; a spec query param could shadow it", f.Name)
		}
	})
	if seen == 0 {
		t.Fatal("addGlobalFlags registered no flags")
	}
}

// Cobra adds --help to every command, and it must stay a bool: a spec param
// that took the name would leave cobra reading a string flag as a bool.
func TestHelpFlagIsReserved(t *testing.T) {
	if !openapi.IsReservedFlagName("help") {
		t.Error("--help must be reserved")
	}
}
