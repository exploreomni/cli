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
	force  bool
	checks int
}

func (f *fakeReleaseChecker) Check(_ context.Context, _ string, force bool) (updatecheck.Result, error) {
	f.force = force
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
	if !checker.force || checker.checks != 1 {
		t.Fatalf("checker force=%v checks=%d", checker.force, checker.checks)
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

func TestUpdateCheckFailure(t *testing.T) {
	checker := &fakeReleaseChecker{err: errors.New("network unavailable")}
	root := &cobra.Command{Use: "omni", SilenceErrors: true, SilenceUsage: true}
	addGlobalFlags(root)
	addUpdateCommand(root, checker, "v1.2.0")
	root.SetArgs([]string{"update", "check"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("error = %v", err)
	}
}
