package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/exploreomni/omni-cli/internal/openapi"
)

// agentHelpBodyExample matches an example invocation with an inline body in
// the agent guide: omni <group> <command> --body '<json>'.
var agentHelpBodyExample = regexp.MustCompile(`omni ([a-z0-9-]+) ([a-z0-9-]+) --body '([^']+)'`)

// Agents copy the guide's examples verbatim, so each one must be a request the
// spec accepts. Placeholders like MODEL_ID stand in for UUIDs.
func TestAgentHelpBodyExamplesPassDryRun(t *testing.T) {
	specData, err := specFS.ReadFile("openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	examples := agentHelpBodyExample.FindAllStringSubmatch(agentHelpText, -1)
	if len(examples) == 0 {
		t.Fatal("no --body examples found in the agent guide")
	}
	for _, ex := range examples {
		group, sub, body := ex[1], ex[2], ex[3]
		body = strings.ReplaceAll(body, "MODEL_ID", "00000000-0000-0000-0000-000000000000")

		cmds, err := openapi.GenerateCommands(specData, func(openapi.APIRequest) error {
			t.Errorf("%s %s: --dry-run reached the executor", group, sub)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var target *bytesCmd
		for _, c := range cmds {
			if c.Name() == group {
				target = &bytesCmd{}
				c.SetOut(&target.out)
				c.SetErr(&target.err)
				c.SilenceUsage = true
				c.SetArgs([]string{sub, "--dry-run", "--body", body})
				if err := c.Execute(); err != nil {
					t.Errorf("omni %s %s --body '%s' fails --dry-run:\n%s", group, sub, ex[3], target.err.String())
				}
			}
		}
		if target == nil {
			t.Errorf("example names unknown group %q", group)
		}
	}
}

type bytesCmd struct {
	out, err bytes.Buffer
}
