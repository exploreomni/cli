package main

import (
	"bytes"
	"strings"
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

// An operation whose spec has a "format" query param gets it as
// --param-format, so --format there is the global output format and must not
// pass silently as JSON.
func TestRootPreRunRejectsUnknownOutputFormat(t *testing.T) {
	tests := []struct {
		args    []string
		env     string
		wantErr string
	}{
		{args: []string{"viz", "--format", "html"}, wantErr: `invalid output format "html" (json, human, auto); this command's "format" query parameter is --param-format`},
		{args: []string{"plain", "-o", "html"}, wantErr: `invalid output format "html" (json, human, auto)`},
		{args: []string{"viz"}, env: "html", wantErr: `invalid OMNI_OUTPUT_FORMAT "html" (json, human, auto)`},
		{args: []string{"viz", "--param-format", "html"}},
		{args: []string{"viz", "-o", "JSON"}},
		{args: []string{"plain"}, env: "Human"},
	}
	for _, tt := range tests {
		t.Setenv("OMNI_OUTPUT_FORMAT", tt.env)
		ran := false
		run := func(*cobra.Command, []string) error { ran = true; return nil }
		root := &cobra.Command{Use: "omni", SilenceErrors: true, PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return checkOutputFormat(cmd)
		}}
		addGlobalFlags(root)
		root.SetGlobalNormalizationFunc(openapi.NormalizeFlagName)
		viz := &cobra.Command{Use: "viz", RunE: run}
		viz.Flags().String("param-format", "", "")
		root.AddCommand(viz, &cobra.Command{Use: "plain", RunE: run})
		var printed bytes.Buffer
		root.SetOut(&printed)
		root.SetErr(&printed)
		root.SetArgs(tt.args)

		err := root.Execute()
		if tt.wantErr == "" {
			if err != nil || !ran {
				t.Errorf("%v (env %q): err %v, ran %v", tt.args, tt.env, err, ran)
			}
			continue
		}
		if err == nil || err.Error() != tt.wantErr {
			t.Errorf("%v (env %q): got error %v, want %q", tt.args, tt.env, err, tt.wantErr)
		}
		if ran {
			t.Errorf("%v: command ran despite an invalid format", tt.args)
		}
		if strings.Contains(printed.String(), "Usage:") {
			t.Errorf("%v: printed usage:\n%s", tt.args, printed.String())
		}
	}
}
