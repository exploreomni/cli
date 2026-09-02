package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/exploreomni/omni-cli/internal/updatecheck"
	"github.com/spf13/cobra"
)

type fakeReleaseChecker struct {
	result updatecheck.Result
	err    error
	checks int
}

func (f *fakeReleaseChecker) Check(_ context.Context, _ string) (updatecheck.Result, error) {
	f.checks++
	return f.result, f.err
}

func TestUpdateCheckJSON(t *testing.T) {
	checker := &fakeReleaseChecker{result: updatecheck.Result{
		UpdateAvailable: true,
		CurrentVersion:  "v1.2.0",
		LatestVersion:   "v1.3.0",
		ReleaseURL:      "https://example.test/v1.3.0",
		Upgrade: updatecheck.UpgradeInstructions{
			Homebrew: "brew upgrade omni",
			Other:    "install command",
		},
	}}
	root := &cobra.Command{Use: "omni"}
	addGlobalFlags(root)
	addUpdateCommand(root, checker, "v1.2.0")
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetArgs([]string{"update", "check", "--format", "json", "--compact"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if checker.checks != 1 {
		t.Fatalf("checks = %d, want one explicit check", checker.checks)
	}
	var got updatecheck.Result
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if !got.UpdateAvailable || got.LatestVersion != "v1.3.0" {
		t.Fatalf("result = %+v", got)
	}
}

func TestUpdateCheckHuman(t *testing.T) {
	checker := &fakeReleaseChecker{result: updatecheck.Result{
		UpdateAvailable: false,
		CurrentVersion:  "v1.3.0",
		LatestVersion:   "v1.3.0",
	}}
	root := &cobra.Command{Use: "omni"}
	addGlobalFlags(root)
	addUpdateCommand(root, checker, "v1.3.0")
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetArgs([]string{"update", "check", "--format", "human"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "v1.3.0 is up to date") {
		t.Fatalf("stdout = %q", got)
	}
}

// A failed check keeps the CLI's stream contract: nothing on stdout, exactly
// one JSON document on stderr, and no second report from cobra.
func TestUpdateCheckFailureJSON(t *testing.T) {
	checker := &fakeReleaseChecker{err: errors.New("network unavailable")}
	root := &cobra.Command{Use: "omni"}
	addGlobalFlags(root)
	addUpdateCommand(root, checker, "v1.2.0")
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"update", "check", "--format", "json", "--compact"})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing on a failure", stdout.String())
	}
	var envelope struct {
		Error  string `json:"error"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("stderr is not a single JSON document (%v): %q", err, stderr.String())
	}
	if envelope.Error != "network unavailable" || envelope.Status != 0 {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestUpdateCheckFailureHuman(t *testing.T) {
	checker := &fakeReleaseChecker{err: errors.New("network unavailable")}
	root := &cobra.Command{Use: "omni"}
	addGlobalFlags(root)
	addUpdateCommand(root, checker, "v1.2.0")
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"update", "check", "--format", "human"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected an error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if got := stderr.String(); got != "Error: network unavailable\n" {
		t.Fatalf("stderr = %q", got)
	}
}
