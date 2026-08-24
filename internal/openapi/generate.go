// Package openapi reads an OpenAPI 3.x spec and generates cobra commands
// for every operation. Path params become positional args, query params
// become flags, and request bodies are read from stdin or flags.
package openapi

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
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
		tagCmd := &cobra.Command{
			Use:   slugify(tag),
			Short: fmt.Sprintf("%s commands", tag),
		}

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
		use += " <" + canonicalFlagName(p.Name) + ">"
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
			// Substitute path params
			path := op.Path
			for i, p := range op.PathParams {
				path = strings.Replace(path, "{"+p.Name+"}", url.PathEscape(args[i]), 1)
			}

			// Build query string from flags.
			//
			// The flag name is the canonical kebab-case rendering of the param
			// (branchId → --branch-id), but the query string must use the
			// param's ORIGINAL spec spelling — query.Set takes q.Name, never
			// the flag name. The server rejects "branch-id" where it expects
			// "branchId", so these two must not be conflated. That holds for
			// params queryFlagName had to rename too: --param-base-url still
			// goes out as baseUrl=.
			query := url.Values{}
			for _, q := range op.QueryParams {
				val, err := cmd.Flags().GetString(queryFlagName(q.Name))
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

	// Accept any spelling of a registered flag that differs only in case or
	// dash/underscore placement, so --branchid and --branchId both hit the
	// canonical --branch-id no matter how the spec spelled the param.
	cmd.Flags().SetNormalizeFunc(NormalizeFlagName)

	// Register query params as flags
	for _, q := range op.QueryParams {
		flagName := queryFlagName(q.Name)
		desc := q.Description
		if len(q.Enum) > 0 {
			desc += fmt.Sprintf(" [%s]", strings.Join(q.Enum, ", "))
		}
		if canonical := canonicalFlagName(q.Name); flagName != canonical {
			// Say so in --help, otherwise the flag looks like a typo next to
			// the spec's parameter name.
			desc = strings.TrimSpace(desc)
			if desc != "" {
				desc += " "
			}
			desc += fmt.Sprintf("(query param %q; renamed because --%s is a global CLI flag)", q.Name, canonical)
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

// canonicalFlagName derives the CLI flag name for an OpenAPI parameter. The
// spec names the same concept inconsistently across sibling operations —
// "branchId" here, "branch_id" there — so camelCase is split into words before
// slugifying and both spellings land on one flag:
//
//	branchId    → branch-id
//	branch_id   → branch-id
//	pageSize    → page-size
//	baseModelId → base-model-id
//
// Acronym runs stay intact: "modelURL" → "model-url", "URLPrefix" → "url-prefix".
func canonicalFlagName(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if i > 0 && unicode.IsUpper(r) {
			prev := rs[i-1]
			switch {
			case !unicode.IsUpper(prev) && prev != '-' && prev != '_' && prev != ' ':
				// lower/digit → upper: a word boundary ("pageSize").
				b.WriteRune('-')
			case unicode.IsUpper(prev) && i+1 < len(rs) && unicode.IsLower(rs[i+1]):
				// End of an acronym run ("URLPrefix" → "URL-Prefix").
				b.WriteRune('-')
			}
		}
		b.WriteRune(r)
	}
	return slugify(b.String())
}

// reservedFlagKeys are the flag names a generated query-param flag must not
// claim, keyed by flagLookupKey. They are either the root command's persistent
// flags (mirrored from addGlobalFlags in cmd/omni/main.go, kept in sync by
// TestGlobalFlagsAreReserved) or flags a generated command registers for
// itself.
//
// Shadowing one of these is not merely confusing, it is unsafe. pflag's
// AddFlagSet skips a persistent flag whose name is already taken locally, so a
// spec param named "baseUrl" would canonicalize to --base-url, take the slot
// before the root's persistent flag merged in, and then resolveConfig's
// GetString("base-url") would read the query param's value — one value both
// filtering the request and choosing the host it is sent to.
var reservedFlagKeys = map[string]string{
	// Root persistent flags.
	flagLookupKey("profile"):  "profile",
	flagLookupKey("token"):    "token",
	flagLookupKey("base-url"): "base-url",
	flagLookupKey("compact"):  "compact",
	flagLookupKey("format"):   "format",
	// Added by cobra on every command; must stay a bool.
	flagLookupKey("help"): "help",
	// Registered by buildCommand for body-taking operations.
	flagLookupKey("body"):      "body",
	flagLookupKey("json-body"): "json-body",
	flagLookupKey("schema"):    "schema",
	flagLookupKey("field"):     "field",
	flagLookupKey("depth"):     "depth",
}

// IsReservedFlagName reports whether name — in any spelling, since matching
// ignores case and dash/underscore placement — is claimed by a global or
// built-in CLI flag.
func IsReservedFlagName(name string) bool {
	_, ok := reservedFlagKeys[flagLookupKey(name)]
	return ok
}

// queryFlagName is the flag name a query param is registered under: its
// canonical kebab-case name, unless that would shadow a global or built-in
// flag, in which case it gets a "param-" prefix (a spec param "baseUrl"
// becomes --param-base-url and leaves --base-url meaning the API endpoint).
//
// Renaming rather than failing generation is deliberate: the spec is synced
// from upstream and can introduce such a param at any time, and a hard failure
// there would take the entire CLI down — every command, not just the affected
// one. Renaming keeps the endpoint reachable and the global flag honest, and
// buildCommand explains the rename in --help.
func queryFlagName(param string) string {
	name := canonicalFlagName(param)
	if IsReservedFlagName(name) {
		return "param-" + name
	}
	return name
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
// ignoring case and dash/underscore placement. So --branchid, --branch_id and
// --branchId all find a registered --branch-id.
//
// Names with no registered match are returned unchanged. That matters at
// registration time: pflag rewrites Flag.Name to whatever this returns, so
// returning the raw lookup key would make --help advertise "--branchid". Help
// output keeps showing only the canonical kebab-case name; the alternates are
// accepted silently.
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

// validateFlagNames reports flags on a single generated command whose names
// collide once normalized. Since lookups ignore case and separators, a command
// declaring both "branchId" and "branch_id" would register two flags that are
// indistinguishable — pflag panics on the second one — so fail generation with
// a message that names the culprits instead.
//
// Query params are checked under their effective flag name, i.e. after
// queryFlagName has moved any that would shadow a global or built-in flag out
// of the way. Body-shorthand flags are hand-written in this repo, so a
// collision there is a bug to fix rather than upstream drift to absorb: they
// are reported, including against the reserved names.
func validateFlagNames(op *operationInfo) error {
	claimed := map[string]string{} // lookup key → description of the claimant

	claim := func(flagName, source string) error {
		key := flagLookupKey(flagName)
		desc := fmt.Sprintf("--%s (%s)", flagName, source)
		if prev, ok := claimed[key]; ok {
			return fmt.Errorf("%s collides with %s: flag names are matched ignoring case, dashes and underscores, so both resolve to %q", desc, prev, key)
		}
		claimed[key] = desc
		return nil
	}

	// Reserved names are spoken for on every command, whether or not this
	// operation registers them itself.
	for key, display := range reservedFlagKeys {
		claimed[key] = fmt.Sprintf("--%s (global or built-in flag)", display)
	}

	for _, q := range op.QueryParams {
		if err := claim(queryFlagName(q.Name), "query param "+q.Name); err != nil {
			return err
		}
	}

	if sh := GetBodyShorthand(op.OperationID); sh != nil {
		for _, f := range sh.Flags {
			if err := claim(f.FlagName, "body shorthand for "+op.OperationID); err != nil {
				return err
			}
		}
	}

	return nil
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
