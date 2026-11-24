package main

import (
	"strings"
	"testing"
)

// The agent guide keeps its OAuth login recipe: an agent reads the device
// code from `config login --format json` and shows it to the person.
func TestAgentHelp_DocumentsOAuthLogin(t *testing.T) {
	for _, want := range []string{"omni config login", "--format json", "verification_uri_complete", "user_code", "login.err", "browser login.", "OMNI_API_TOKEN"} {
		if !strings.Contains(agentHelpText, want) {
			t.Errorf("agent help doesn't mention %q", want)
		}
	}
}
