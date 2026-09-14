package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/exploreomni/omni-cli/internal/auth"
	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/exploreomni/omni-cli/internal/output"
	"github.com/exploreomni/omni-cli/internal/updatecheck"
	"github.com/exploreomni/omni-cli/internal/useragent"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

//go:embed openapi.json
var specFS embed.FS

var version = "dev"

func init() {
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
			version = info.Main.Version
		}
	}
}

func main() {
	useragent.Set(version)

	checker := updatecheck.New()
	var updater automaticUpdate
	root := &cobra.Command{
		Use:     "omni",
		Short:   "Omni CLI — programmatic access to the Omni API",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// This hook runs immediately before RunE, so the check overlaps
			// with the work the user asked for.
			updater = startAutomaticUpdate(checker, version, cmd, os.Stdout, os.Stderr)

			// Skip auth for config commands
			if cmd.Name() == "init" || cmd.Name() == "show" || cmd.Name() == "use" || cmd.Name() == "login" || cmd.Name() == "logout" || cmd.Name() == "config" {
				return nil
			}
			// Skip auth for help/version
			if cmd.Name() == "agent-help" {
				return nil
			}
			if cmd.Name() == "help" || cmd.Name() == "version" {
				return nil
			}
			return nil
		},
	}

	addGlobalFlags(root)

	// Flag names are matched ignoring case and dash/underscore placement, so
	// --base-url, --baseurl and --base_url are the same flag on every command.
	// Set before the subcommands are added: cobra propagates the function to
	// children as they're attached, which covers the hand-written commands and
	// the root's persistent flags as well as the generated ones.
	root.SetGlobalNormalizationFunc(openapi.NormalizeFlagName)

	// Hand-written commands (not from spec)
	addConfigCommands(root)
	addAgentHelpCommand(root)
	addUpdateCommand(root, checker, version)

	// Load OpenAPI spec and generate API commands
	specData, err := specFS.ReadFile("openapi.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load embedded API spec: %v\n", err)
		os.Exit(1)
	}

	apiCmds, err := openapi.GenerateCommands(specData, executeAPICall)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to parse API spec: %v\n", err)
		os.Exit(1)
	}

	for _, cmd := range apiCmds {
		addResultFlags(cmd)
		root.AddCommand(cmd)
	}

	// Hand-written commands that attach to generated command groups
	addBranchCommands(root, executeAPICall)
	addUserCommands(root, executeAPICall)

	// ExecuteC, not Execute: cobra returns a nil error whenever it answers the
	// help flag, including for `omni models list-branches --help`, where the
	// "help" is really an unknown-subcommand error. UnknownSubcommand asks the
	// command that ran whether that's what happened.
	cmd, err := root.ExecuteC()
	success := err == nil && !openapi.UnknownSubcommand(cmd)
	updater.finish(success, os.Stderr)
	if !success {
		os.Exit(1)
	}
}

// addGlobalFlags registers the flags every command inherits.
//
// These names are reserved: openapi.IsReservedFlagName knows them, so a spec
// query param that would otherwise shadow one (say a param named "baseUrl"
// taking over --base-url) is registered under a --param- prefix instead.
// TestGlobalFlagsAreReserved fails if a flag is added here without being
// added there too.
func addGlobalFlags(root *cobra.Command) {
	root.PersistentFlags().StringP("profile", "p", "", "config profile to use")
	root.PersistentFlags().String("token", "", "API token (overrides profile/env)")
	root.PersistentFlags().String("base-url", "", "API base URL (overrides profile)")
	root.PersistentFlags().Bool("compact", false, "compact JSON output (no indentation)")
	root.PersistentFlags().StringP("format", "o", "", "output format: json, human, auto (default auto: human on TTY, json when piped)")
}

// addResultFlags registers the presentation flags on an API command group
// (not the root, so `config init --chart` is an error). Names are reserved
// in openapi.globalFlagKeys.
func addResultFlags(cmd *cobra.Command) {
	f := cmd.PersistentFlags()
	f.Bool("workbook", false, "also open the query in an ephemeral workbook and print its link")
	f.String("chart", "", "draw query results as a chart (--chart or --chart=bar)")
	f.Lookup("chart").NoOptDefVal = output.ChartKindBar
	f.String("chart-label", "", "only this dimension labels the rows, by field or label (default: every dimension)")
	f.String("chart-value", "", "only this measure gets bars, by field or label (default: every measure)")
	f.Int("chart-rows", output.DefaultChartRows, "most rows to draw before summarising the rest")
	f.String("chart-style", output.StyleBar, "bar style: bar, block (solid, finer ends), line, fill (value inside the bar; rows touch)")
}

// chartOptions reads the --chart flags; nil when no chart was asked for. An
// explicitly chosen JSON format (flag, env, config — not a pipe's auto
// detection) refuses a chart.
func chartOptions(cmd *cobra.Command, chosenFormat string) (*output.ChartOptions, error) {
	kind, err := cmd.Flags().GetString("chart")
	if err != nil || kind == "" {
		return nil, nil
	}
	if kind != output.ChartKindBar {
		return nil, fmt.Errorf("unknown chart kind %q (supported: %s)", kind, output.ChartKindBar)
	}
	if chosenFormat == config.FormatJSON {
		return nil, fmt.Errorf("--chart cannot be combined with JSON output: a chart is not JSON")
	}
	label, _ := cmd.Flags().GetString("chart-label")
	value, _ := cmd.Flags().GetString("chart-value")
	rows, _ := cmd.Flags().GetInt("chart-rows")
	style, _ := cmd.Flags().GetString("chart-style")
	if !output.ValidStyle(style) {
		return nil, fmt.Errorf("--chart-style %q is not one of bar, block, line, fill", style)
	}
	return &output.ChartOptions{
		Kind:    kind,
		Label:   label,
		Value:   value,
		Width:   terminalWidth(),
		MaxRows: rows,
		Style:   style,
	}, nil
}

// prepareBody applies --chart (drops any resultType, with a note) and
// --workbook (sets workbookUrl) to a JSON body, touching only fields the
// command's spec declares.
func prepareBody(chart, workbook bool, format string, cmd *cobra.Command, body []byte) ([]byte, error) {
	if !chart && !workbook {
		return body, nil
	}
	if workbook && !openapi.BodyDeclares(cmd, "workbookUrl") {
		return nil, fmt.Errorf("--workbook is not supported by %s", cmd.CommandPath())
	}
	if len(bytes.TrimSpace(body)) == 0 {
		if workbook {
			return nil, fmt.Errorf("--workbook needs a JSON request body to set workbookUrl on; pass one with --body or on stdin")
		}
		return body, nil
	}
	if !openapi.BodyDeclares(cmd, "resultType") && !workbook {
		return body, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		// Silently dropping the flag would send the request without
		// workbookUrl and leave the user wondering where their link went.
		if workbook {
			return nil, fmt.Errorf("--workbook needs a JSON object as the request body: %w", err)
		}
		return body, nil
	}
	changed := false
	if raw, ok := obj["resultType"]; chart && ok {
		if format == config.FormatHuman {
			fmt.Fprintf(os.Stderr, "note: --chart ignores \"resultType\": %s and reads the query stream\n", raw)
		}
		delete(obj, "resultType")
		changed = true
	}
	if !workbook {
		if !changed {
			return body, nil
		}
		filled, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("preparing the request body: %w", err)
		}
		return filled, nil
	}
	if isTrue(obj["planOnly"]) {
		return nil, fmt.Errorf("--workbook cannot be combined with planOnly")
	}
	if obj == nil {
		return nil, fmt.Errorf("--workbook needs a JSON object as the request body")
	}
	obj["workbookUrl"] = json.RawMessage(`true`)
	filled, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("preparing the request body: %w", err)
	}
	return filled, nil
}

// printWorkbookLink surfaces the X-Omni-Workbook-Url header. It joins
// human-rendered output on stdout; a passed-through body (CSV, XLSX) or
// JSON output keeps stdout as the payload, so the link goes to stderr.
func printWorkbookLink(resp *http.Response, format string, compact bool, stdout, stderr io.Writer) {
	u := resp.Header.Get("X-Omni-Workbook-Url")
	if u == "" {
		return
	}
	rendered := strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json")
	switch {
	case format == config.FormatHuman && rendered:
		output.ChartLink(stdout, u)
	case format == config.FormatHuman:
		output.ChartLink(stderr, u)
	default:
		raw, _ := json.Marshal(map[string]string{"workbookUrl": u})
		_ = output.JSONBytes(stderr, raw, compact)
	}
}

func isTrue(raw json.RawMessage) bool {
	var b bool
	return json.Unmarshal(raw, &b) == nil && b
}

func terminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	if w < 40 {
		return 40
	}
	if w > 160 {
		return 160
	}
	return w
}

// executeAPICall is the callback invoked by generated commands to make the actual HTTP request.
func executeAPICall(req openapi.APIRequest) error {
	cfg, err := resolveConfig(req.Cmd)
	if err != nil {
		return err
	}

	compact, _ := req.Cmd.Flags().GetBool("compact")
	formatFlag, _ := req.Cmd.Flags().GetString("format")
	chosen := config.ChosenOutputFormat(formatFlag)
	format := config.FormatFromChoice(chosen, term.IsTerminal(int(os.Stdout.Fd())))

	chart, err := chartOptions(req.Cmd, chosen)
	if err != nil {
		return err
	}
	if chart != nil && !openapi.ReturnsStream(req.Cmd) {
		return fmt.Errorf("--chart plots query results: use it with query run or query wait")
	}
	workbook, _ := req.Cmd.Flags().GetBool("workbook")
	req.Body, err = prepareBody(chart != nil, workbook, format, req.Cmd, req.Body)
	if err != nil {
		return err
	}

	// Show a spinner on stderr while the request is in flight. Only when the
	// user is at an interactive terminal AND they're going to see human output;
	// scripts piping JSON shouldn't get decorative noise on stderr.
	sp := maybeStartSpinner(format)

	resp, err := auth.DoWithContentType(cfg, req.Method, req.Path, req.Body, req.ContentType)
	sp.Stop()
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// A query stream rendered for a person is decoded — labels, formats,
	// dimensions from the model — and waited on to completion. For JSON it
	// passes through untouched, as the output contract promises.
	if isQueryStream(resp) && (chart != nil || format == config.FormatHuman) {
		err = renderStream(cfg, resp, format, compact, chart, os.Stdout, os.Stderr)
	} else {
		err = outputResponse(resp, format, compact, chart)
		if err == nil {
			printWorkbookLink(resp, format, compact, os.Stdout, os.Stderr)
		}
	}
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		// outputResponse already wrote a complete error message to stderr —
		// a JSON envelope carrying the status, or the human-mode one-liner.
		// Letting cobra append its own line would say it twice, and would
		// leave two documents on stderr for JSON consumers to trip over.
		req.Cmd.SilenceErrors = true
	}
	return err
}

// resolveConfig builds the runtime config from flags, env, and config file.
func resolveConfig(cmd *cobra.Command) (*config.ResolvedConfig, error) {
	profileName, _ := cmd.Flags().GetString("profile")
	tokenFlag, _ := cmd.Flags().GetString("token")
	baseURLFlag, _ := cmd.Flags().GetString("base-url")

	return config.Resolve(profileName, tokenFlag, baseURLFlag)
}
