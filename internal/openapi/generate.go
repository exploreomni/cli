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
	"strconv"
	"strings"
	"unicode"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
			if err := validateFlagNames(op); err != nil {
				return nil, fmt.Errorf("generating command %q: %w", slugify(tag)+" "+commandName(op), err)
			}
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
	Response    *responseInfo     // success (2xx) response, when the spec declares one
	Deprecated  bool
}

// responseInfo captures the operation's success response so --schema can show
// callers what shape comes back — otherwise invisible without making a call.
type responseInfo struct {
	Status      string
	ContentType string
	Description string
	Schema      *base.SchemaProxy // nil when the status declares no body/schema
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

		info.Response = successResponse(op.Responses)

		groups[tag] = append(groups[tag], info)
	}
}

func buildCommand(op *operationInfo, exec Executor) *cobra.Command {
	// Build the use string: operation-name <path-param1> <path-param2> ...
	name := commandName(op)
	queryFlags := resolveQueryFlags(op)
	use := name
	for _, p := range op.PathParams {
		use += " <" + canonicalName(p.Name) + ">"
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

			// Build query string from flags.
			//
			// The flag name is the canonical kebab-case rendering of the param
			// (branchId → --branch-id), but the query string must use the
			// param's ORIGINAL spec spelling — the server rejects "branch-id"
			// where it expects "branchId", so the two must not be conflated.
			// That holds for renamed params too: --param-base-url still goes
			// out as baseUrl=.
			query := url.Values{}
			for _, qf := range queryFlags {
				val, err := cmd.Flags().GetString(qf.Name)
				if err != nil {
					continue
				}
				if val != "" {
					query.Set(qf.Param.Name, val)
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

	// Accept any spelling of a registered flag that differs only in case or
	// dash/underscore placement, so --branchid and --branchId both hit the
	// canonical --branch-id no matter how the spec spelled the param.
	cmd.Flags().SetNormalizeFunc(NormalizeFlagName)

	// Register query params as flags
	for _, qf := range queryFlags {
		desc := qf.Param.Description
		if len(qf.Param.Enum) > 0 {
			desc += fmt.Sprintf(" [%s]", strings.Join(qf.Param.Enum, ", "))
		}
		if qf.Note != "" {
			// Say so in --help, otherwise the flag looks like a typo next to
			// the spec's parameter name.
			desc = strings.TrimSpace(desc)
			if desc != "" {
				desc += " "
			}
			desc += qf.Note
		}
		cmd.Flags().String(qf.Name, "", desc)
	}

	// If the operation accepts a body, add --body and --json-body flags
	if op.HasBody {
		cmd.Flags().String("body", "", `request body as JSON string, or "-" for stdin (run with --schema to see its shape)`)
		cmd.Flags().String("json-body", "", `request body as JSON string, or "-" for stdin (alias for --body)`)
		cmd.Flags().MarkHidden("json-body")
	}

	// Apply body shorthand if one exists for this operation
	sh := GetBodyShorthand(op.OperationID)
	if sh != nil {
		applyBodyShorthand(cmd, op, sh)
	}

	// Describe the positional args in the long help. Cobra's usage line only
	// shows the placeholders, so without this the spec's param descriptions
	// (e.g. "branch name", not "branch UUID") never reach the reader.
	if args := argumentsHelp(op, sh); args != "" {
		if cmd.Long == "" {
			cmd.Long = short
		}
		cmd.Long = strings.TrimRight(cmd.Long, "\n") + "\n\n" + args
	}

	// Add the --schema discovery flag last, after any shorthand has replaced
	// Args/RunE, so the short-circuit wraps the final versions. Registered for
	// every operation — bodyless ones still describe their args, query flags and
	// response shape.
	RegisterSchemaFlag(cmd, func(c *cobra.Command, names SchemaFlags) error {
		return emitBodySchema(c, op, names)
	})

	return cmd
}

// argumentsHelp renders an "Arguments:" block listing every positional the
// command accepts — path params first (in URL order), then any body-shorthand
// args — with the description the spec gives for each. Returns "" when the
// command takes no positionals.
func argumentsHelp(op *operationInfo, sh *BodyShorthand) string {
	type argLine struct{ name, desc string }
	var lines []argLine

	for _, p := range op.PathParams {
		lines = append(lines, argLine{"<" + slugify(p.Name) + ">", firstLine(p.Description)})
	}
	if sh != nil {
		for _, a := range sh.Args {
			lines = append(lines, argLine{"<" + a.Name + ">", firstLine(a.Description)})
		}
	}
	if len(lines) == 0 {
		return ""
	}

	width := 0
	for _, l := range lines {
		if len(l.name) > width {
			width = len(l.name)
		}
	}

	var b strings.Builder
	b.WriteString("Arguments:")
	for _, l := range lines {
		b.WriteString("\n  " + l.name)
		if l.desc != "" {
			b.WriteString(strings.Repeat(" ", width-len(l.name)) + "  " + l.desc)
		}
	}
	return b.String()
}

// firstLine keeps the Arguments block to one line per arg.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// schemaRequested reports whether the schema discovery flag — registered under
// name, which is not always "schema" (see RegisterSchemaFlag) — is set.
func schemaRequested(cmd *cobra.Command, name string) bool {
	v, err := cmd.Flags().GetBool(name)
	return err == nil && v
}

// requestBodySchema returns the schema for a request body, preferring the
// application/json media type and falling back to the first declared one.
func requestBodySchema(rb *v3.RequestBody) *base.SchemaProxy {
	if rb == nil {
		return nil
	}
	_, schema := pickMediaType(rb.Content)
	return schema
}

// successResponse returns the operation's success response: the lowest declared
// 2xx status (the happy path; a lower code wins so 200 beats a 202 fallback),
// falling back to an OpenAPI range key ("2XX") when only that is declared — a
// specific code always beats the wildcard. It returns nil when no success status
// is declared at all, and a schema-less responseInfo when the status carries no
// body (e.g. 204).
func successResponse(resps *v3.Responses) *responseInfo {
	if resps == nil || resps.Codes == nil {
		return nil
	}

	best := 0
	var bestResp *v3.Response
	var bestCode string
	var wildcardResp *v3.Response
	var wildcardCode string

	for pair := resps.Codes.First(); pair != nil; pair = pair.Next() {
		key := pair.Key()
		if isSuccessRange(key) {
			if wildcardResp == nil {
				wildcardCode, wildcardResp = key, pair.Value()
			}
			continue
		}
		code, err := strconv.Atoi(key)
		if err != nil || code < 200 || code > 299 {
			continue
		}
		if bestResp == nil || code < best {
			best, bestCode, bestResp = code, key, pair.Value()
		}
	}
	if bestResp == nil {
		bestCode, bestResp = wildcardCode, wildcardResp
	}
	if bestResp == nil {
		return nil
	}

	info := &responseInfo{Status: bestCode, Description: bestResp.Description}
	info.ContentType, info.Schema = pickMediaType(bestResp.Content)
	return info
}

// isSuccessRange reports whether a response key is the OpenAPI 2xx range
// wildcard. The spec writes ranges uppercase ("2XX"), but tolerate any casing.
func isSuccessRange(key string) bool {
	return len(key) == 3 && key[0] == '2' && (key[1] == 'X' || key[1] == 'x') && (key[2] == 'X' || key[2] == 'x')
}

// pickMediaType chooses which declared media type to report, preferring
// application/json and then the first type carrying a schema. A media type that
// declares no schema is still reported by name with a nil schema: "this comes
// back as text/csv, shape undocumented" beats implying the response is empty.
func pickMediaType(content *orderedmap.Map[string, *v3.MediaType]) (string, *base.SchemaProxy) {
	if content == nil {
		return "", nil
	}

	var jsonType string
	var jsonSchema *base.SchemaProxy
	var firstWithSchema string
	var firstSchema *base.SchemaProxy
	var firstAny string

	for pair := content.First(); pair != nil; pair = pair.Next() {
		key := pair.Key()
		var schema *base.SchemaProxy
		if mt := pair.Value(); mt != nil {
			schema = mt.Schema
		}
		if key == "application/json" {
			if schema != nil {
				return key, schema
			}
			if jsonType == "" {
				jsonType, jsonSchema = key, schema
			}
		}
		if schema != nil && firstWithSchema == "" {
			firstWithSchema, firstSchema = key, schema
		}
		if firstAny == "" {
			firstAny = key
		}
	}

	switch {
	case firstWithSchema != "":
		return firstWithSchema, firstSchema
	case jsonType != "":
		return jsonType, jsonSchema
	default:
		return firstAny, nil
	}
}

// commandName derives a CLI subcommand name from the operationId or method+path.
func commandName(op *operationInfo) string {
	if op.OperationID != "" {
		// Strip the tag prefix if present (e.g., "models-list" from "ModelsList")
		name := canonicalName(op.OperationID)
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

// canonicalName renders an OpenAPI identifier — a parameter name or an
// operationId — as the kebab-case spelling the CLI uses for flags and command
// names. The spec names the same concept inconsistently across sibling
// operations ("branchId" here, "branch_id" there), so camelCase is split into
// words before slugifying and both spellings land on one flag:
//
//	branchId    → branch-id
//	branch_id   → branch-id
//	baseModelId → base-model-id
//	ModelsList  → models-list
//
// Acronym runs stay intact ("modelURL" → "model-url", "URLPrefix" →
// "url-prefix"), including when pluralized ("apiURLs" → "api-urls").
func canonicalName(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if i > 0 && unicode.IsUpper(r) {
			prev := rs[i-1]
			switch {
			case !unicode.IsUpper(prev) && prev != '-' && prev != '_' && prev != ' ':
				// lower/digit → upper: a word boundary ("pageSize").
				b.WriteRune('-')
			case unicode.IsUpper(prev) && i+1 < len(rs) && unicode.IsLower(rs[i+1]) && !isPluralS(rs, i+1):
				// End of an acronym run ("URLPrefix" → "URL-Prefix"), unless the
				// lowercase letter is just the acronym's plural ("URLs").
				b.WriteRune('-')
			}
		}
		b.WriteRune(r)
	}
	return slugify(b.String())
}

// isPluralS reports whether rs[i] is a lone trailing "s" pluralizing the
// acronym before it, as in "URLs" or "IDsList" — as opposed to the start of a
// following word, as in "JSONString".
func isPluralS(rs []rune, i int) bool {
	return rs[i] == 's' && (i+1 == len(rs) || !unicode.IsLower(rs[i+1]))
}

// globalFlagKeys are the flag names a generated query-param flag must never
// claim, keyed by flagLookupKey: the root command's persistent flags (mirrored
// from addGlobalFlags in cmd/omni/main.go, kept in sync by
// TestGlobalFlagsAreReserved) plus the --help flag cobra adds everywhere.
//
// Shadowing one of these is not merely confusing, it is unsafe: pflag's
// AddFlagSet skips a persistent flag whose name is already taken locally, so a
// spec param named "baseUrl" would take the --base-url slot before the root's
// persistent flag merged in, and resolveConfig's GetString("base-url") would
// then read the query param's value — one value both filtering the request and
// choosing the host it is sent to.
var globalFlagKeys = map[string]string{
	// Root persistent flags.
	flagLookupKey("profile"):  "profile",
	flagLookupKey("token"):    "token",
	flagLookupKey("base-url"): "base-url",
	flagLookupKey("compact"):  "compact",
	flagLookupKey("format"):   "format",
	// Added by cobra on every command; must stay a bool.
	flagLookupKey("help"): "help",
}

// bodyFlagKeys are the flags buildCommand registers for itself, but only on
// operations that take a request body. They are therefore reserved only on
// those commands: a bodyless operation with a param named "field" keeps
// --field, since nothing else on that command wants it.
var bodyFlagKeys = map[string]string{
	flagLookupKey("body"):      "body",
	flagLookupKey("json-body"): "json-body",
	flagLookupKey("schema"):    "schema",
	flagLookupKey("field"):     "field",
	flagLookupKey("depth"):     "depth",
}

// IsReservedFlagName reports whether name — in any spelling, since matching
// ignores case and dash/underscore placement — is claimed by a global or
// built-in flag present on every command. Flags that only some commands
// register (--body and friends) are handled per-operation by reservedFlagsFor.
func IsReservedFlagName(name string) bool {
	_, ok := globalFlagKeys[flagLookupKey(name)]
	return ok
}

// reservedFlagsFor returns the flag names a generated command claims for
// itself, keyed by flagLookupKey and mapped to the phrase --help uses when a
// query param has to be renamed around one. Globals are reserved on every
// command, --body and friends only on body-taking operations, and body
// shorthands only on the operation they are written for.
func reservedFlagsFor(op *operationInfo) map[string]string {
	reserved := map[string]string{}
	for key, name := range globalFlagKeys {
		reserved[key] = fmt.Sprintf("--%s is a global CLI flag", name)
	}
	if op.HasBody {
		for key, name := range bodyFlagKeys {
			reserved[key] = fmt.Sprintf("--%s is a built-in flag", name)
		}
	}
	if sh := GetBodyShorthand(op.OperationID); sh != nil {
		for _, f := range sh.Flags {
			reserved[flagLookupKey(f.FlagName)] = fmt.Sprintf("--%s is a body shorthand flag", f.FlagName)
		}
	}
	return reserved
}

// queryFlag is a query param together with the flag name it is actually
// registered under and, when that is not the param's canonical name, the
// explanation buildCommand puts in --help.
type queryFlag struct {
	Param paramInfo
	Name  string
	Note  string
}

// resolveQueryFlags assigns every query param on an operation a flag name that
// is free: the canonical kebab-case name, "param-"-prefixed if the command
// already claims that name (a global flag, --body and friends, a body
// shorthand), then "-2", "-3"... if an earlier param on the same operation got
// there first.
//
// Renaming rather than failing generation is deliberate: the spec is synced
// from upstream and can introduce a colliding param at any time, and a hard
// failure here would take the entire CLI down — every command, not just the
// affected one.
func resolveQueryFlags(op *operationInfo) []queryFlag {
	reserved := reservedFlagsFor(op)
	claimed := map[string]bool{}
	for key := range reserved {
		claimed[key] = true
	}

	flags := make([]queryFlag, 0, len(op.QueryParams))
	for _, q := range op.QueryParams {
		canonical := canonicalName(q.Name)
		base, note := canonical, ""
		if reason, ok := reserved[flagLookupKey(canonical)]; ok {
			base = "param-" + canonical
			note = fmt.Sprintf("(query param %q; renamed because %s)", q.Name, reason)
		}

		name := base
		if claimed[flagLookupKey(name)] {
			if note == "" {
				note = fmt.Sprintf("(query param %q; renamed because another param on this command also becomes --%s)", q.Name, base)
			}
			for n := 2; claimed[flagLookupKey(name)]; n++ {
				name = fmt.Sprintf("%s-%d", base, n)
			}
		}

		claimed[flagLookupKey(name)] = true
		flags = append(flags, queryFlag{Param: q, Name: name, Note: note})
	}
	return flags
}

// flagLookupKey reduces a flag name to the form used for matching: lowercased,
// with dashes and underscores dropped. "branchId", "branch-id", "branch_id"
// and "branchid" all reduce to "branchid".
func flagLookupKey(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r == '-' || r == '_' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// NormalizeFlagName is a pflag normalization function that resolves a flag name
// the user typed to whichever flag is registered under an equivalent spelling,
// ignoring case and dash/underscore placement, so --branchid, --branch_id and
// --branchId all find a registered --branch-id.
//
// Names with no registered match are returned unchanged, which matters at
// registration time: pflag rewrites Flag.Name to whatever this returns, so
// returning the raw lookup key would make --help advertise "--branchid".
func NormalizeFlagName(fs *pflag.FlagSet, name string) pflag.NormalizedName {
	key := flagLookupKey(name)
	match := ""
	fs.VisitAll(func(f *pflag.Flag) {
		if match == "" && flagLookupKey(f.Name) == key {
			match = f.Name
		}
	})
	if match != "" {
		return pflag.NormalizedName(match)
	}
	return pflag.NormalizedName(name)
}

// validateFlagNames reports a body-shorthand flag that cannot be registered:
// one colliding with another shorthand on the same operation, or with a global
// or built-in flag. Shorthands are hand-written in this repo, so that is a bug
// to fix here — query params come from the synced spec and are renamed around
// the shorthands by resolveQueryFlags instead.
func validateFlagNames(op *operationInfo) error {
	sh := GetBodyShorthand(op.OperationID)
	if sh == nil {
		return nil
	}

	claimed := map[string]string{} // lookup key → description of the claimant
	for key, display := range globalFlagKeys {
		claimed[key] = fmt.Sprintf("--%s (global or built-in flag)", display)
	}
	if op.HasBody {
		for key, display := range bodyFlagKeys {
			claimed[key] = fmt.Sprintf("--%s (built-in flag)", display)
		}
	}

	for _, f := range sh.Flags {
		key := flagLookupKey(f.FlagName)
		desc := fmt.Sprintf("--%s (body shorthand for %s)", f.FlagName, op.OperationID)
		if prev, ok := claimed[key]; ok {
			return fmt.Errorf("%s collides with %s: flag names are matched ignoring case, dashes and underscores, so both resolve to %q", desc, prev, key)
		}
		claimed[key] = desc
	}

	return nil
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
