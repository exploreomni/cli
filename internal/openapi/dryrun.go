package openapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// DryRunResult is what a passing --dry-run prints to stdout: the request that
// would have been sent. BodyChecked is false when there was no body to check or
// its media type is not JSON (a multipart upload is checked field by field when
// it is assembled, not against the schema).
type DryRunResult struct {
	Valid       bool   `json:"valid"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	BodyChecked bool   `json:"bodyChecked"`
}

// DryRunError is the single JSON document a failing --dry-run writes to stderr.
// It extends the CLI's error envelope ({"error": ...}) with every violation, so
// a caller fixes them all in one pass.
type DryRunError struct {
	Error      string      `json:"error"`
	Violations []Violation `json:"violations"`
}

// registerDryRunFlag adds --dry-run (or a collision-free fallback, following
// the same policy as the --schema flags) and returns the name it got.
func registerDryRunFlag(cmd *cobra.Command) string {
	name := freeFlagName(cmd, "dry-run", "validate-only")
	cmd.Flags().Bool(name, false, renameNote(
		"check the request against the embedded API spec and exit without sending it (no auth, no network); reports every problem at once",
		"dry-run", name))
	return name
}

func dryRun(cmd *cobra.Command, name string) bool {
	if name == "" {
		return false
	}
	on, _ := cmd.Flags().GetBool(name)
	return on
}

// emitDryRun validates the assembled request: query flags against their enums,
// and a JSON body against the operation's request schema. A clean request is
// printed to stdout; otherwise one JSON document listing every violation goes
// to stderr and the command fails, exactly like a rejected API call would,
// minus the round trip.
func emitDryRun(cmd *cobra.Command, op *operationInfo, queryFlags []queryFlag, query url.Values, path string, body []byte, bodyProvided bool) error {
	var violations []Violation

	for _, qf := range queryFlags {
		if len(qf.Param.Enum) == 0 {
			continue
		}
		val := query.Get(qf.Param.Name)
		if val == "" || containsString(qf.Param.Enum, val) {
			continue
		}
		msg := fmt.Sprintf("must be one of %s, got %q", strings.Join(qf.Param.Enum, ", "), val)
		if s := suggest(val, append([]string(nil), qf.Param.Enum...)); s != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", s)
		}
		violations = append(violations, Violation{Path: "--" + qf.Name, Message: msg})
	}

	checked := false
	if op.HasBody && op.BodyMediaType != "multipart/form-data" && op.bodyFlagIsJSON() {
		switch {
		case bodyProvided:
			checked = true
			violations = append(violations, ValidateBody(op.BodySchema, body)...)
		case op.BodyRequired:
			violations = append(violations, Violation{Path: "body", Message: "request body is required; pass it with --body (run with --schema to see its shape)"})
		}
	}

	compact, _ := cmd.Flags().GetBool("compact")
	marshal := func(v interface{}) ([]byte, error) {
		if compact {
			return json.Marshal(v)
		}
		return json.MarshalIndent(v, "", "  ")
	}

	if len(violations) == 0 {
		data, err := marshal(DryRunResult{Valid: true, Method: op.Method, Path: path, BodyChecked: checked})
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	noun := "problems"
	if len(violations) == 1 {
		noun = "problem"
	}
	data, err := marshal(DryRunError{
		Error:      fmt.Sprintf("request failed validation (%d %s); nothing was sent", len(violations), noun),
		Violations: violations,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(), string(data))
	// The document above is the whole report; cobra's own "Error:" line would
	// put a second, non-JSON document on stderr.
	cmd.SilenceErrors = true
	return errDryRunFailed
}

// errDryRunFailed makes the command exit non-zero after a failed --dry-run has
// already written its report.
var errDryRunFailed = fmt.Errorf("request failed validation")

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
