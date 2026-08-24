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
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
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

	// Add the --schema discovery flag last, after any shorthand has replaced
	// Args/RunE, so the short-circuit wraps the final versions. Registered for
	// every operation — bodyless ones still describe their args, query flags and
	// response shape.
	RegisterSchemaFlag(cmd, func(c *cobra.Command, names SchemaFlags) error {
		return emitBodySchema(c, op, names)
	})

	return cmd
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
