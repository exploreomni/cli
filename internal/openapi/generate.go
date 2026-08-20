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

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/spf13/cobra"
)

// APIRequest is passed to the executor callback when a generated command runs.
type APIRequest struct {
	Cmd         *cobra.Command
	Method      string
	Path        string // fully resolved path with query string
	Body        []byte // nil for GET/DELETE
	ContentType string // request body media type; defaults to application/json when empty
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
	Tag           string
	OperationID   string
	Summary       string
	Description   string
	Method        string
	Path          string
	PathParams    []paramInfo
	QueryParams   []paramInfo
	HasBody       bool
	BodySchema    *base.SchemaProxy // request body schema, when HasBody
	BodyMediaType string
	BodyFields    []multipartFieldInfo
	Deprecated    bool
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
			mediaType, media := requestBodyMediaType(op.RequestBody)
			info.BodyMediaType = mediaType
			if media != nil {
				info.BodySchema = media.Schema
				if mediaType == "multipart/form-data" {
					info.BodyFields = multipartFields(media.Schema, media.Encoding)
				}
			}
		}

		groups[tag] = append(groups[tag], info)
	}
}

func buildCommand(op *operationInfo, exec Executor) *cobra.Command {
	// Build the use string: operation-name <path-param1> <path-param2> ...
	name := commandName(op)
	use := name
	for _, p := range op.PathParams {
		use += " <" + cliFlagName(p.Name) + ">"
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

			// Build query string from flags
			query := url.Values{}
			for _, q := range op.QueryParams {
				val, err := queryFlagValue(cmd, q.Name)
				if err != nil {
					return err
				}
				if val != "" {
					query.Set(q.Name, val)
				}
			}
			if len(query) > 0 {
				path += "?" + query.Encode()
			}

			// Read body from stdin, a file, or the flag value itself.
			var body []byte
			contentType := op.BodyMediaType
			if op.HasBody {
				bodyFlag, _ := cmd.Flags().GetString("body")
				jsonBodyFlag, _ := cmd.Flags().GetString("json-body")

				if bodyFlag != "" && jsonBodyFlag != "" {
					return fmt.Errorf("cannot use both --body and --json-body; use one or the other")
				}

				effectiveBody, flagName := bodyFlag, "body"
				bodyProvided := cmd.Flags().Changed("body")
				if jsonBodyFlag != "" {
					effectiveBody, flagName = jsonBodyFlag, "json-body"
					bodyProvided = true
				}

				if effectiveBody != "" {
					var err error
					body, err = resolveBody(effectiveBody, flagName, op.bodyFlagIsJSON())
					if err != nil {
						// A body input error is self-explanatory; the usage
						// block would bury the hint.
						cmd.SilenceUsage = true
						return err
					}
				}

				if op.BodyMediaType == "multipart/form-data" {
					var err error
					body, contentType, err = buildMultipartBody(cmd, body, bodyProvided, op.BodyFields)
					if err != nil {
						return err
					}
				}
			}

			return exec(APIRequest{
				Cmd:         cmd,
				Method:      op.Method,
				Path:        path,
				Body:        body,
				ContentType: contentType,
			})
		},
	}

	// Register query params as flags
	for _, q := range op.QueryParams {
		flagName := cliFlagName(q.Name)
		desc := q.Description
		if len(q.Enum) > 0 {
			desc += fmt.Sprintf(" [%s]", strings.Join(q.Enum, ", "))
		}
		cmd.Flags().String(flagName, "", desc)

		// Before camelCase names were normalized, flags such as modelId were
		// exposed as --modelid. Keep a hidden deprecated alias so existing
		// scripts continue to work while help and new usage use --model-id.
		legacyName := slugify(q.Name)
		if legacyName != flagName {
			cmd.Flags().String(legacyName, "", "deprecated alias for --"+flagName)
			_ = cmd.Flags().MarkDeprecated(legacyName, "use --"+flagName+" instead")
			_ = cmd.Flags().MarkHidden(legacyName)
		}
	}

	// If the operation accepts a body, add --body and --json-body flags.
	if op.HasBody {
		bodyHelp := `request body as JSON string, "@path/to/file.json" to read a file, or "-" for stdin (run with --schema to see its shape)`
		if op.BodyMediaType == "multipart/form-data" {
			bodyHelp = `multipart fields as JSON string, "@path/to/file.json", or "-" for stdin; binary field values are file paths`
		}
		cmd.Flags().String("body", "", bodyHelp)
		cmd.Flags().String("json-body", "", `request body as JSON string, "@path/to/file.json", or "-" for stdin (alias for --body)`)
		cmd.Flags().MarkHidden("json-body")

		if op.BodyMediaType == "multipart/form-data" {
			registerMultipartFlags(cmd, op.BodyFields)
		}
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

// requestBodyMediaType returns the media type and definition the CLI will use,
// preferring application/json for backward compatibility and otherwise using
// the first declared media type.
func requestBodyMediaType(rb *v3.RequestBody) (string, *v3.MediaType) {
	if rb == nil || rb.Content == nil {
		return "", nil
	}
	var firstType string
	var first *v3.MediaType
	for pair := rb.Content.First(); pair != nil; pair = pair.Next() {
		mt := pair.Value()
		if mt == nil {
			continue
		}
		if pair.Key() == "application/json" {
			return pair.Key(), mt
		}
		if first == nil {
			firstType = pair.Key()
			first = mt
		}
	}
	return firstType, first
}

// bodyFlagIsJSON reports whether the --body flag value is JSON, and so worth
// validating client-side. A JSON body is sent verbatim; a multipart body takes
// a JSON object of field values that buildMultipartBody turns into form parts.
// A body with no declared content defaults to JSON — that's what the CLI sends.
// Any other media type passes --body through as raw bytes, unvalidated.
func (op *operationInfo) bodyFlagIsJSON() bool {
	switch {
	case op.BodyMediaType == "", op.BodyMediaType == "application/json":
		return true
	case op.BodyMediaType == "multipart/form-data":
		return true
	}
	return strings.HasSuffix(op.BodyMediaType, "+json")
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

func cliFlagName(s string) string {
	return slugify(camelToKebab(s))
}

func queryFlagValue(cmd *cobra.Command, parameterName string) (string, error) {
	canonicalName := cliFlagName(parameterName)
	legacyName := slugify(parameterName)
	canonicalChanged := cmd.Flags().Changed(canonicalName)
	legacyChanged := legacyName != canonicalName && cmd.Flags().Changed(legacyName)
	if canonicalChanged && legacyChanged {
		return "", fmt.Errorf("cannot use both --%s and deprecated --%s", canonicalName, legacyName)
	}
	if legacyChanged {
		return cmd.Flags().GetString(legacyName)
	}
	return cmd.Flags().GetString(canonicalName)
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
