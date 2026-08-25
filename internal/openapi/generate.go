// Package openapi reads an OpenAPI 3.x spec and generates cobra commands
// for every operation. Path params become positional args, query params
// become flags, and request bodies are read from stdin or flags.
package openapi

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/spf13/cobra"
)

// APIRequest is passed to the executor callback when a generated command runs.
type APIRequest struct {
	Cmd    *cobra.Command
	Method string
	Path   string // fully resolved path with query string
	Body   []byte // nil for GET/DELETE
}

// Executor is the callback that actually makes the HTTP request.
type Executor func(req APIRequest) error

// GenerateCommands parses an OpenAPI spec and returns cobra commands grouped by tag.
func GenerateCommands(specData []byte, exec Executor) ([]*cobra.Command, error) {
	doc, err := libopenapi.NewDocument(specData)
	if err != nil {
		return nil, fmt.Errorf("parsing openapi spec: %w", err)
	}

	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("building openapi model: %w", err)
	}

	// Group operations by tag → subcommand groups
	groups := map[string][]*operationInfo{}

	if model.Model.Paths != nil && model.Model.Paths.PathItems != nil {
		for pair := model.Model.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
			pathStr := pair.Key()
			pathItem := pair.Value()
			extractOperations(pathStr, pathItem, groups)
		}
	}

	// Build cobra commands: one parent per tag, one child per operation
	var cmds []*cobra.Command
	tagNames := sortedKeys(groups)

	for _, tag := range tagNames {
		ops := groups[tag]
		tagCmd := NewGroupCommand(slugify(tag), fmt.Sprintf("%s commands", tag))

		for _, op := range ops {
			tagCmd.AddCommand(buildCommand(op, exec))
		}

		cmds = append(cmds, tagCmd)
	}

	return cmds, nil
}

const (
	// groupAnnotation marks a command as a subcommand group, so the shared
	// help func knows which commands to apply unknown-subcommand handling to.
	groupAnnotation = "omni/group"

	// unknownSubcommandAnnotation records that a command printed an
	// unknown-subcommand error instead of help. Cobra's help path always
	// returns nil from Execute, so the caller has to read this to exit
	// non-zero — see UnknownSubcommand.
	unknownSubcommandAnnotation = "omni/unknown-subcommand"
)

// NewGroupCommand builds a command that only groups subcommands (e.g. `omni
// models`), with the handling that keeps a mistyped or missing subcommand from
// looking like success: an unknown subcommand is a cobra-style error with
// suggestions, and a bare group prints its help to stderr — both exit non-zero
// with nothing on stdout. That covers `omni models list-branches --help`, which
// is a typo rather than a help request; plain `omni <group> --help` is
// unaffected (help on stdout, exit 0).
func NewGroupCommand(use, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:         use,
		Short:       short,
		Annotations: map[string]string{groupAnnotation: "true"},
		RunE:        GroupRunE,
	}
	cmd.SetHelpFunc(groupHelpFunc)
	return cmd
}

// GroupRunE is the RunE for a command built by NewGroupCommand.
func GroupRunE(cmd *cobra.Command, args []string) error {
	// Past flag parsing: from here on, errors are runtime errors, not usage
	// errors, so don't bury the message under the usage block.
	cmd.SilenceUsage = true

	if len(args) == 0 {
		// The group produced no data, so its help goes to stderr and stdout
		// stays empty for the pipe. The help text is the whole error report:
		// letting cobra append its own "Error: ..." line would state the same
		// failure twice, so silence it and let the non-zero exit speak.
		renderHelp(cmd.ErrOrStderr(), cmd)
		cmd.SilenceErrors = true
		return fmt.Errorf("%q requires a subcommand", cmd.CommandPath())
	}

	return errors.New(unknownSubcommandMessage(cmd, args[0]))
}

// groupHelpFunc backs --help for group commands and, by inheritance, for their
// subcommands. It only diverges from cobra's default when a group is asked for
// help with an unresolved positional argument left over — `omni models
// list-branches --help` — which is a typo, not a help request.
func groupHelpFunc(cmd *cobra.Command, _ []string) {
	leftover := cmd.Flags().Args()
	if !isGroup(cmd) || len(leftover) == 0 {
		renderHelp(cmd.OutOrStdout(), cmd)
		return
	}

	cmd.Annotations[unknownSubcommandAnnotation] = leftover[0]
	fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", cmd.ErrPrefix(), unknownSubcommandMessage(cmd, leftover[0]))
}

// UnknownSubcommand reports whether cmd's help output was really an
// unknown-subcommand error. Cobra returns a nil error from Execute whenever it
// handles the help flag, so main has to ask before choosing an exit code.
func UnknownSubcommand(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd.Annotations[unknownSubcommandAnnotation]
	return ok
}

func isGroup(cmd *cobra.Command) bool {
	return cmd != nil && cmd.Annotations[groupAnnotation] == "true"
}

func unknownSubcommandMessage(cmd *cobra.Command, typed string) string {
	var msg strings.Builder
	fmt.Fprintf(&msg, "unknown subcommand %q for %q", typed, cmd.CommandPath())
	if names := subcommandSuggestions(cmd, typed); len(names) > 0 {
		msg.WriteString("\n\nDid you mean this?")
		for _, n := range names {
			fmt.Fprintf(&msg, "\n\t%s", n)
		}
	}
	fmt.Fprintf(&msg, "\n\nRun '%s --help' for a list of available subcommands", cmd.CommandPath())
	return msg.String()
}

// renderHelp writes cobra's stock help text for cmd to w. It mirrors cobra's
// defaultHelpFunc rather than calling cmd.Help(), which dispatches back through
// HelpFunc — i.e. straight back into groupHelpFunc, forever.
func renderHelp(w io.Writer, cmd *cobra.Command) {
	desc := cmd.Long
	if desc == "" {
		desc = cmd.Short
	}
	if desc = strings.TrimRight(desc, " \t\n"); desc != "" {
		fmt.Fprintf(w, "%s\n\n", desc)
	}
	if cmd.Runnable() || cmd.HasSubCommands() {
		fmt.Fprint(w, cmd.UsageString())
	}
}

// subcommandSuggestions returns close matches for a mistyped subcommand. It
// extends cobra's Levenshtein/prefix matching with a reverse-prefix pass so an
// over-specified guess like `models list-branches` still points at `list`
// (the real answer being `list --model-kind BRANCH`).
func subcommandSuggestions(cmd *cobra.Command, typed string) []string {
	if cmd.DisableSuggestions {
		return nil
	}
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}

	seen := map[string]bool{}
	var names []string
	for _, s := range cmd.SuggestionsFor(typed) {
		if !seen[s] {
			seen[s] = true
			names = append(names, s)
		}
	}
	for _, sub := range cmd.Commands() {
		if !sub.IsAvailableCommand() || seen[sub.Name()] {
			continue
		}
		if strings.HasPrefix(strings.ToLower(typed), strings.ToLower(sub.Name())+"-") {
			seen[sub.Name()] = true
			names = append(names, sub.Name())
		}
	}
	return names
}

type paramInfo struct {
	Name        string
	In          string // path, query, header
	Description string
	Required    bool
	Type        string // string, integer, boolean, number
	Enum        []string
}

type operationInfo struct {
	Tag         string
	OperationID string
	Summary     string
	Description string
	Method      string
	Path        string
	PathParams  []paramInfo
	QueryParams []paramInfo
	HasBody     bool
	BodySchema  *base.SchemaProxy // request body schema, when HasBody
	Deprecated  bool
}

func extractOperations(pathStr string, item *v3.PathItem, groups map[string][]*operationInfo) {
	methods := map[string]*v3.Operation{
		"GET":    item.Get,
		"POST":   item.Post,
		"PUT":    item.Put,
		"DELETE": item.Delete,
		"PATCH":  item.Patch,
	}

	for method, op := range methods {
		if op == nil {
			continue
		}

		tag := "misc"
		if len(op.Tags) > 0 {
			tag = op.Tags[0]
		}

		info := &operationInfo{
			Tag:         tag,
			OperationID: op.OperationId,
			Summary:     op.Summary,
			Description: op.Description,
			Method:      method,
			Path:        pathStr,
			Deprecated:  boolVal(op.Deprecated),
		}

		// Collect parameters
		for _, p := range op.Parameters {
			pi := paramInfo{
				Name:        p.Name,
				In:          p.In,
				Description: p.Description,
				Required:    boolVal(p.Required),
				Type:        schemaType(p.Schema),
			}
			if p.Schema != nil && p.Schema.Schema() != nil {
				for _, e := range p.Schema.Schema().Enum {
					if e != nil {
						pi.Enum = append(pi.Enum, fmt.Sprintf("%v", e.Value))
					}
				}
			}
			switch p.In {
			case "path":
				info.PathParams = append(info.PathParams, pi)
			case "query":
				info.QueryParams = append(info.QueryParams, pi)
			}
		}

		// Positional args follow the URL shape, not the spec's parameters
		// array — some endpoints declare path params out of path order.
		sort.SliceStable(info.PathParams, func(i, j int) bool {
			return strings.Index(pathStr, "{"+info.PathParams[i].Name+"}") <
				strings.Index(pathStr, "{"+info.PathParams[j].Name+"}")
		})

		// Check for request body
		if op.RequestBody != nil {
			info.HasBody = true
			info.BodySchema = requestBodySchema(op.RequestBody)
		}

		groups[tag] = append(groups[tag], info)
	}
}

func buildCommand(op *operationInfo, exec Executor) *cobra.Command {
	// Build the use string: operation-name <path-param1> <path-param2> ...
	name := commandName(op)
	use := name
	for _, p := range op.PathParams {
		use += " <" + slugify(p.Name) + ">"
	}

	short := op.Summary
	if short == "" {
		short = fmt.Sprintf("%s %s", op.Method, op.Path)
	}

	long := op.Description
	if op.Deprecated {
		long = "DEPRECATED: " + long
	}

	cmd := &cobra.Command{
		Use:        use,
		Short:      short,
		Long:       long,
		Deprecated: deprecatedMsg(op),
		Args:       cobra.ExactArgs(len(op.PathParams)),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Flags parsed and args validated: anything that fails from here on
			// (bad body, HTTP 4xx/5xx) is a runtime error, and dumping the usage
			// block after it just buries the message — and, when the caller is
			// piping, mixes prose into the stream.
			cmd.SilenceUsage = true

			// Substitute path params
			path := op.Path
			for i, p := range op.PathParams {
				path = strings.Replace(path, "{"+p.Name+"}", url.PathEscape(args[i]), 1)
			}

			// Build query string from flags
			query := url.Values{}
			for _, q := range op.QueryParams {
				flagName := slugify(q.Name)
				val, err := cmd.Flags().GetString(flagName)
				if err != nil {
					continue
				}
				if val != "" {
					query.Set(q.Name, val)
				}
			}
			if len(query) > 0 {
				path += "?" + query.Encode()
			}

			// Read body from stdin or flags
			var body []byte
			if op.HasBody {
				bodyFlag, _ := cmd.Flags().GetString("body")
				jsonBodyFlag, _ := cmd.Flags().GetString("json-body")

				if bodyFlag != "" && jsonBodyFlag != "" {
					return fmt.Errorf("cannot use both --body and --json-body; use one or the other")
				}

				effectiveBody := bodyFlag
				if jsonBodyFlag != "" {
					effectiveBody = jsonBodyFlag
				}

				if effectiveBody == "-" || effectiveBody == "" {
					if effectiveBody == "-" {
						var err error
						body, err = readStdin()
						if err != nil {
							return fmt.Errorf("reading stdin: %w", err)
						}
					}
				} else if effectiveBody != "" {
					body = []byte(effectiveBody)
				}
			}

			return exec(APIRequest{
				Cmd:    cmd,
				Method: op.Method,
				Path:   path,
				Body:   body,
			})
		},
	}

	// Register query params as flags
	for _, q := range op.QueryParams {
		flagName := slugify(q.Name)
		desc := q.Description
		if len(q.Enum) > 0 {
			desc += fmt.Sprintf(" [%s]", strings.Join(q.Enum, ", "))
		}
		cmd.Flags().String(flagName, "", desc)
	}

	// If the operation accepts a body, add --body and --json-body flags
	if op.HasBody {
		cmd.Flags().String("body", "", `request body as JSON string, or "-" for stdin (run with --schema to see its shape)`)
		cmd.Flags().String("json-body", "", `request body as JSON string, or "-" for stdin (alias for --body)`)
		cmd.Flags().MarkHidden("json-body")
	}

	// Apply body shorthand if one exists for this operation
	if sh := GetBodyShorthand(op.OperationID); sh != nil {
		applyBodyShorthand(cmd, op, sh)
	}

	// Add the --schema discovery flag last, wrapping arg validation and RunE so
	// it short-circuits before any positional-arg checks, body assembly, auth,
	// or network call. This lets `omni <cmd> --schema` work with no args/token.
	if op.HasBody {
		cmd.Flags().Bool("schema", false, "print the request body's JSON schema and a filled-in example, then exit (no API call)")
		// --field / --depth refine the --schema output for deeply nested bodies.
		// Guarded so a future query/path param of the same name can't panic the
		// flag registration.
		if cmd.Flags().Lookup("field") == nil {
			cmd.Flags().String("field", "", "with --schema: drill into a dotted field path (e.g. queryPresentations.data.query); auto-descends arrays and maps")
		}
		if cmd.Flags().Lookup("depth") == nil {
			cmd.Flags().Int("depth", maxSchemaDepth, "with --schema: max nesting depth to expand; lower for a compact overview")
		}

		innerArgs := cmd.Args
		cmd.Args = func(c *cobra.Command, args []string) error {
			if schemaRequested(c) || innerArgs == nil {
				return nil
			}
			return innerArgs(c, args)
		}

		innerRun := cmd.RunE
		cmd.RunE = func(c *cobra.Command, args []string) error {
			if schemaRequested(c) {
				// A schema error (e.g. a bad --field path) should print just the
				// helpful message, not the full usage block.
				c.SilenceUsage = true
				return emitBodySchema(c, op)
			}
			return innerRun(c, args)
		}
	}

	return cmd
}

// schemaRequested reports whether the --schema discovery flag is set.
func schemaRequested(cmd *cobra.Command) bool {
	v, err := cmd.Flags().GetBool("schema")
	return err == nil && v
}

// requestBodySchema returns the schema for a request body, preferring the
// application/json media type and falling back to the first declared one.
func requestBodySchema(rb *v3.RequestBody) *base.SchemaProxy {
	if rb == nil || rb.Content == nil {
		return nil
	}
	var first *base.SchemaProxy
	for pair := rb.Content.First(); pair != nil; pair = pair.Next() {
		mt := pair.Value()
		if mt == nil || mt.Schema == nil {
			continue
		}
		if pair.Key() == "application/json" {
			return mt.Schema
		}
		if first == nil {
			first = mt.Schema
		}
	}
	return first
}

// commandName derives a CLI subcommand name from the operationId or method+path.
func commandName(op *operationInfo) string {
	if op.OperationID != "" {
		// Strip the tag prefix if present (e.g., "models-list" from "ModelsList")
		name := camelToKebab(op.OperationID)
		tagPrefix := slugify(op.Tag) + "-"
		name = strings.TrimPrefix(name, tagPrefix)
		return name
	}
	// Fallback: method + last path segment
	parts := strings.Split(strings.Trim(op.Path, "/"), "/")
	last := parts[len(parts)-1]
	if strings.HasPrefix(last, "{") {
		if len(parts) >= 2 {
			last = parts[len(parts)-2]
		}
	}
	return strings.ToLower(op.Method) + "-" + slugify(last)
}

// Helper functions

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

func camelToKebab(s string) string {
	var result strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(r + 32) // toLower
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func schemaType(proxy *base.SchemaProxy) string {
	if proxy == nil {
		return "string"
	}
	s := proxy.Schema()
	if s == nil {
		return "string"
	}
	if len(s.Type) > 0 {
		return s.Type[0]
	}
	return "string"
}

func boolVal(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

func deprecatedMsg(op *operationInfo) string {
	if op.Deprecated {
		return "this operation is deprecated"
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

const maxStdinSize = 10 << 20 // 10 MB

func readStdin() ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, maxStdinSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxStdinSize {
		return nil, fmt.Errorf("stdin input exceeds maximum size of 10 MB")
	}
	return data, nil
}
