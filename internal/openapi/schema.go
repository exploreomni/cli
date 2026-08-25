package openapi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"
)

// maxSchemaDepth caps how deep we expand nested objects when describing a
// request body or response. Some shapes (notably the v2 document content blob)
// nest very deeply; without a cap a single --schema dump could be megabytes.
// Beyond this depth we emit a short placeholder noting the omission rather than
// recursing.
const maxSchemaDepth = 8

// depthNote replaces a schema node that sits past the expansion budget. Both the
// spec describer and the depth limiter for hand-written commands' static
// documents emit it, so truncation reads identically everywhere.
const depthNote = "max depth reached; expansion omitted"

// SchemaArg describes a positional argument, derived from a spec path param.
type SchemaArg struct {
	Name        string `json:"name"`
	Placeholder string `json:"placeholder"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

// SchemaQueryParam describes a query parameter as it is exposed on the CLI.
type SchemaQueryParam struct {
	Flag        string   `json:"flag"`
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Required    bool     `json:"required"`
	Description string   `json:"description,omitempty"`
}

// SchemaResponse describes the operation's success response: the lowest 2xx
// status the spec declares, its media type, and the simplified schema. Schema is
// nil when that status carries no body (e.g. 204) or declares no schema.
type SchemaResponse struct {
	Status      string      `json:"status,omitempty"`
	ContentType string      `json:"contentType,omitempty"`
	Description string      `json:"description,omitempty"`
	Schema      interface{} `json:"schema"`
}

// SchemaDoc is the JSON document emitted by `omni <cmd> --schema`: the full call
// contract — positional args, query flags, the request body (shape plus a
// copy-pasteable Example) and the success response shape. Body and Response are
// always present, explicitly null when the operation takes no body / the spec
// declares no 2xx status, so absence is never ambiguous. SchemaFlags appears
// only when a spec parameter forced a discovery flag off its preferred name.
type SchemaDoc struct {
	Method      string             `json:"method"`
	Path        string             `json:"path"`
	SchemaFlags *SchemaFlags       `json:"schemaFlags,omitempty"`
	Field       string             `json:"field,omitempty"`
	Args        []SchemaArg        `json:"args,omitempty"`
	QueryParams []SchemaQueryParam `json:"queryParams,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Body        interface{}        `json:"body"`
	Example     interface{}        `json:"example,omitempty"`
	Response    *SchemaResponse    `json:"response"`
}

// describer carries the per-invocation expansion budget so --depth can override
// the default cap without threading it through every recursive call or reaching
// for a package global (which would not be concurrency-safe under tests).
type describer struct {
	maxDepth int
}

// SchemaFlags carries the flag names the discovery flags were actually
// registered under. They are the preferred names (schema/field/depth) unless a
// spec parameter already owns one, in which case the discovery flag falls back
// to a prefixed name. The emit callback must read the flags through these names
// rather than hardcoding them.
type SchemaFlags struct {
	Schema string `json:"schema"`
	Field  string `json:"field"`
	Depth  string `json:"depth"`
}

// Renamed reports whether any discovery flag had to fall back off its preferred
// name because a spec parameter owns it.
func (f SchemaFlags) Renamed() bool {
	return f.Schema != "schema" || f.Field != "field" || f.Depth != "depth"
}

// RegisterSchemaFlag adds the --schema discovery flag (plus its --field and
// --depth refinements) to cmd and wraps arg validation and RunE so --schema
// short-circuits before any positional-arg check, body assembly, auth or network
// call, calling emit with the resolved flag names instead. Every command
// registers it, bodyless ones included: agents reach for --schema first, and
// "unknown flag: --schema" wastes a whole call.
//
// Collision policy: a spec parameter always keeps its own flag name — silently
// reinterpreting e.g. an endpoint's --depth query param as a discovery control
// would corrupt requests. A colliding discovery flag is instead registered under
// a deterministic "schema-" prefixed fallback (--schema-doc / --schema-field /
// --schema-depth, and -2, -3… if those are taken too) which its help text and
// the document's schemaFlags both name, so discovery is never unavailable.
func RegisterSchemaFlag(cmd *cobra.Command, emit func(*cobra.Command, SchemaFlags) error) SchemaFlags {
	names := SchemaFlags{
		Schema: freeFlagName(cmd, "schema", "schema-doc"),
		Field:  freeFlagName(cmd, "field", "schema-field"),
		Depth:  freeFlagName(cmd, "depth", "schema-depth"),
	}

	cmd.Flags().Bool(names.Schema, false,
		renameNote("print this command's args, flags, request body and response shape, then exit (no API call)", "schema", names.Schema))
	// --field / --depth refine the --schema output for deeply nested shapes.
	cmd.Flags().String(names.Field, "",
		renameNote(fmt.Sprintf("with --%s: drill into a dotted field path of the request body (e.g. queryPresentations.data.query); auto-descends arrays and maps; does not affect the response section", names.Schema), "field", names.Field))
	cmd.Flags().Int(names.Depth, maxSchemaDepth,
		renameNote(fmt.Sprintf("with --%s: max nesting depth to expand (request body and response); lower for a compact overview", names.Schema), "depth", names.Depth))

	// Cobra prints a Deprecated notice at the top of execute() — before flags are
	// even parsed — which would put plain text ahead of --schema's JSON. Take
	// ownership of the notice: clear the field so cobra stays quiet, keep the
	// command out of help listings the way Deprecated did (via Hidden), and
	// re-print the notice to stderr ourselves on real runs only.
	deprecated := cmd.Deprecated
	if deprecated != "" {
		cmd.Deprecated = ""
		cmd.Hidden = true
	}

	innerArgs := cmd.Args
	cmd.Args = func(c *cobra.Command, args []string) error {
		if schemaRequested(c, names.Schema) || innerArgs == nil {
			return nil
		}
		return innerArgs(c, args)
	}

	innerRun := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if schemaRequested(c, names.Schema) {
			// A schema error (e.g. a bad --field path) should print just the
			// helpful message, not the full usage block.
			c.SilenceUsage = true
			return emit(c, names)
		}
		if deprecated != "" {
			fmt.Fprintf(c.ErrOrStderr(), "Command %q is deprecated, %s\n", c.Name(), deprecated)
		}
		if innerRun == nil {
			return nil
		}
		return innerRun(c, args)
	}

	return names
}

// freeFlagName returns preferred if no flag owns it, otherwise fallback, and
// otherwise fallback with the lowest "-N" suffix that is free. Registration
// therefore always succeeds — a discovery flag never silently disappears, and a
// spec parameter is never shadowed.
func freeFlagName(cmd *cobra.Command, preferred, fallback string) string {
	if cmd.Flags().Lookup(preferred) == nil {
		return preferred
	}
	if cmd.Flags().Lookup(fallback) == nil {
		return fallback
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", fallback, n)
		if cmd.Flags().Lookup(candidate) == nil {
			return candidate
		}
	}
}

// renameNote appends an explanation to a discovery flag's help when it had to
// give up its preferred name, so `--help` still leads the reader to it.
func renameNote(desc, preferred, actual string) string {
	if preferred == actual {
		return desc
	}
	return fmt.Sprintf("%s (named --%s here because --%s is a parameter of this endpoint)", desc, actual, preferred)
}

// emitBodySchema writes a generated operation's schema document to the command's
// stdout, honoring the global --compact flag. It makes no network call and needs
// no auth. The optional --field flag drills into a dotted sub-path of the request
// body; --depth caps how deep nested objects expand.
func emitBodySchema(cmd *cobra.Command, op *operationInfo, names SchemaFlags) error {
	field, _ := cmd.Flags().GetString(names.Field)

	doc, err := describeBody(op, field, schemaDepth(cmd, names.Depth), names)
	if err != nil {
		return err
	}
	return EmitSchemaDoc(cmd, doc)
}

// StaticSchemaEmitter returns a RegisterSchemaFlag callback for a hand-written
// command that documents itself with a static document (build). Such a command
// assembles its request body from its own flags rather than passing JSON
// through, so --field has nothing to drill into and is rejected by name; --depth
// is applied to the document so it truncates exactly as it does on generated
// commands.
func StaticSchemaEmitter(cmdName string, build func() SchemaDoc) func(*cobra.Command, SchemaFlags) error {
	return func(c *cobra.Command, names SchemaFlags) error {
		if field, _ := c.Flags().GetString(names.Field); field != "" {
			return fmt.Errorf("--%s is not supported for %s; its request body is assembled by the CLI and shown in full", names.Field, cmdName)
		}
		return EmitSchemaDoc(c, limitDocDepth(build(), schemaDepth(c, names.Depth)))
	}
}

// limitDocDepth truncates an already-materialized schema document to maxDepth,
// mirroring what the describer does while walking the spec. It is how a static
// document honors --depth without a second implementation of the placeholder
// convention.
func limitDocDepth(doc SchemaDoc, maxDepth int) SchemaDoc {
	doc.Body = limitSchemaDepth(doc.Body, 0, maxDepth)
	doc.Example = limitValueDepth(doc.Example, 0, maxDepth)
	if doc.Response != nil {
		resp := *doc.Response
		resp.Schema = limitSchemaDepth(resp.Schema, 0, maxDepth)
		doc.Response = &resp
	}
	return doc
}

// limitSchemaDepth walks a plain-Go schema map the way simplify walks a spec
// schema — properties, items, additionalProperties and union members each nest
// one level — and replaces anything past maxDepth with the depthNote
// placeholder, keeping the node's type and $ref like simplify does.
func limitSchemaDepth(v interface{}, depth, maxDepth int) interface{} {
	node, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	if depth > maxDepth {
		out := map[string]interface{}{"note": depthNote}
		if t, ok := node["type"]; ok {
			out["type"] = t
		}
		if r, ok := node["$ref"]; ok {
			out["$ref"] = r
		}
		return out
	}

	out := make(map[string]interface{}, len(node))
	for k, val := range node {
		switch k {
		case "properties":
			if props, ok := val.(map[string]interface{}); ok {
				limited := make(map[string]interface{}, len(props))
				for name, p := range props {
					limited[name] = limitSchemaDepth(p, depth+1, maxDepth)
				}
				out[k] = limited
				continue
			}
		case "items", "additionalProperties":
			out[k] = limitSchemaDepth(val, depth+1, maxDepth)
			continue
		case "oneOf", "anyOf":
			if members, ok := val.([]interface{}); ok {
				limited := make([]interface{}, 0, len(members))
				for _, m := range members {
					limited = append(limited, limitSchemaDepth(m, depth+1, maxDepth))
				}
				out[k] = limited
				continue
			}
		}
		out[k] = val
	}
	return out
}

// limitValueDepth truncates an example value the way synth does: an object past
// the budget collapses to {}, while scalars and array wrappers are kept.
func limitValueDepth(v interface{}, depth, maxDepth int) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		if depth > maxDepth {
			return map[string]interface{}{}
		}
		out := make(map[string]interface{}, len(val))
		for k, item := range val {
			out[k] = limitValueDepth(item, depth+1, maxDepth)
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(val))
		for _, item := range val {
			out = append(out, limitValueDepth(item, depth+1, maxDepth))
		}
		return out
	}
	return v
}

// EmitSchemaDoc writes a schema document as JSON to the command's stdout,
// honoring the global --compact flag. Hand-written commands use it to emit the
// same document shape as generated ones.
func EmitSchemaDoc(cmd *cobra.Command, doc SchemaDoc) error {
	compact, _ := cmd.Flags().GetBool("compact")
	var (
		data []byte
		err  error
	)
	if compact {
		data, err = json.Marshal(doc)
	} else {
		data, err = json.MarshalIndent(doc, "", "  ")
	}
	if err != nil {
		return fmt.Errorf("encoding schema: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

// schemaDepth reads the depth flag, falling back to the default cap when it is
// unset or nonsensical.
func schemaDepth(cmd *cobra.Command, name string) int {
	depth, err := cmd.Flags().GetInt(name)
	if err != nil || depth < 0 {
		return maxSchemaDepth
	}
	return depth
}

// describeBody builds the schema document for an operation: its positional args,
// query flags, request body and success response. When field is non-empty it
// drills to that dotted sub-path of the request body; maxDepth caps nested
// expansion. Drilling restarts the depth budget from the resolved node, so a deep
// field still expands fully.
func describeBody(op *operationInfo, field string, maxDepth int, names SchemaFlags) (SchemaDoc, error) {
	doc := SchemaDoc{Method: op.Method, Path: op.Path, Field: field}
	if names.Renamed() {
		// Surface the fallback names in the document too, so an agent that got
		// here via --schema-doc learns what to pass for the refinements.
		renamed := names
		doc.SchemaFlags = &renamed
	}
	d := &describer{maxDepth: maxDepth}

	for _, p := range op.PathParams {
		doc.Args = append(doc.Args, SchemaArg{
			Name:        p.Name,
			Placeholder: "<" + slugify(p.Name) + ">",
			Type:        p.Type,
			Description: p.Description,
		})
	}
	for _, q := range op.QueryParams {
		doc.QueryParams = append(doc.QueryParams, SchemaQueryParam{
			Flag:        "--" + slugify(q.Name),
			Name:        q.Name,
			Type:        q.Type,
			Enum:        q.Enum,
			Required:    q.Required,
			Description: q.Description,
		})
	}
	// Resolve --field before describing anything: an invalid path is an error, and
	// the response schema alone can run to hundreds of kilobytes.
	root := op.BodySchema
	if root == nil {
		if field != "" {
			return doc, fmt.Errorf("--%s is only meaningful for operations with a request body schema; %s %s has none", names.Field, op.Method, op.Path)
		}
	} else if field != "" {
		resolved, err := resolveField(root, field, names.Field)
		if err != nil {
			return doc, err
		}
		root = resolved
	}

	doc.Response = describeResponse(d, op.Response)

	if root == nil {
		// Body stays nil (rendered as null) so a bodyless operation is explicit
		// rather than ambiguous. An operation that declares a body but no schema
		// says so instead, so null always means "sends nothing".
		if op.HasBody {
			doc.Body = map[string]interface{}{
				"note": "this operation takes a request body, but the spec declares no schema for it",
			}
		}
		return doc, nil
	}

	body := d.simplify(root, 0, nil)
	doc.Body = body
	if m, ok := body.(map[string]interface{}); ok {
		if req, ok := m["required"].([]string); ok {
			doc.Required = req
		}
	}
	doc.Example = d.synth(root, "", 0, nil)
	return doc, nil
}

// describeResponse renders the captured success response. --field deliberately
// does not apply here: it drills the request body only.
func describeResponse(d *describer, resp *responseInfo) *SchemaResponse {
	if resp == nil {
		return nil
	}
	out := &SchemaResponse{
		Status:      resp.Status,
		ContentType: resp.ContentType,
		Description: resp.Description,
	}
	if resp.Schema != nil {
		out.Schema = d.simplify(resp.Schema, 0, nil)
	}
	return out
}

// resolveField walks a dotted path (e.g. "queryPresentations.data.query") from
// the request-body schema to a sub-schema. A plain segment selects an object
// property (flattening allOf). When a segment doesn't name a property, the
// walker transparently descends through array items and map
// (additionalProperties) values and retries — so callers can write
// "queryPresentations.data.query" without knowing data is a map keyed by tab
// ID. It returns the resolved proxy, or an error naming the failing segment and
// listing the fields available there.
func resolveField(root *base.SchemaProxy, path, flagName string) (*base.SchemaProxy, error) {
	cur := root
	segs := strings.Split(path, ".")
	for i, seg := range segs {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			return nil, fmt.Errorf("--%s %q has an empty path segment", flagName, path)
		}
		next, err := descendTo(cur, seg)
		if err != nil {
			return nil, fmt.Errorf("--%s %q: %v", flagName, strings.Join(segs[:i+1], "."), err)
		}
		cur = next
	}
	return cur, nil
}

// descendTo finds the schema for property `seg` reachable from proxy, unwrapping
// any array/map container layers in between. The unwrap loop is bounded by the
// set of $refs already visited so a recursive container can't spin forever.
func descendTo(proxy *base.SchemaProxy, seg string) (*base.SchemaProxy, error) {
	seen := map[string]bool{}
	for {
		if proxy == nil {
			return nil, fmt.Errorf("no field %q", seg)
		}
		if proxy.IsReference() {
			ref := proxy.GetReference()
			if seen[ref] {
				return nil, fmt.Errorf("no field %q", seg)
			}
			seen[ref] = true
		}
		sch := proxy.Schema()
		if sch == nil {
			return nil, fmt.Errorf("no field %q", seg)
		}

		_, props := gatherObject(sch, nil)
		if p, ok := props[seg]; ok {
			return p, nil
		}

		// Not a direct property — unwrap one container layer and retry the same
		// segment one level in (arrays carry their value in items, maps in
		// additionalProperties).
		switch {
		case sch.Items != nil && sch.Items.IsA():
			proxy = sch.Items.A
		case sch.AdditionalProperties != nil && sch.AdditionalProperties.IsA():
			proxy = sch.AdditionalProperties.A
		default:
			return nil, fieldNotFoundErr(seg, props, sch)
		}
	}
}

// fieldNotFoundErr explains a failed path lookup, listing the fields available
// at that point so an agent can correct the path in one step.
func fieldNotFoundErr(seg string, props map[string]*base.SchemaProxy, sch *base.Schema) error {
	switch {
	case len(props) > 0:
		return fmt.Errorf("no field %q; available: %s", seg, strings.Join(sortedKeys(props), ", "))
	case len(sch.OneOf) > 0 || len(sch.AnyOf) > 0:
		return fmt.Errorf("no field %q; this is a union (oneOf/anyOf), drill not supported here", seg)
	default:
		return fmt.Errorf("no field %q; %v has no named fields", seg, joinTypes(sch.Type))
	}
}

// allOfShapeKeys are the non-object schema facets simplify adopts from allOf
// members so a typed ref wrapped in an allOf (the `.describe()`-on-a-ref
// pattern) keeps its shape instead of collapsing to an empty object. properties
// and required are merged separately, so they are deliberately excluded here. A
// later member's value overrides an earlier one's; the schema's own facets,
// applied after the allOf loop, override these in turn.
var allOfShapeKeys = []string{
	"type", "items", "anyOf", "oneOf", "additionalProperties",
	"enum", "format", "description", "default", "example",
}

// simplify turns a libopenapi schema into a compact, agent-friendly map. It
// merges allOf composition into a single object, preserves descriptions, enums,
// formats, required fields and examples, and guards against deep nesting (via
// d.maxDepth) and recursive $refs. `seen` tracks the $refs already expanded on
// the current path so a self-referential schema (e.g. folder → children →
// folder) stops instead of looping forever.
func (d *describer) simplify(proxy *base.SchemaProxy, depth int, seen map[string]bool) interface{} {
	if proxy == nil {
		return nil
	}

	ref := ""
	if proxy.IsReference() {
		ref = proxy.GetReference()
		if seen[ref] {
			return map[string]interface{}{"$ref": ref, "note": "recursive reference; expansion omitted"}
		}
	}

	sch := proxy.Schema()
	if sch == nil {
		if ref != "" {
			return map[string]interface{}{"$ref": ref}
		}
		return nil
	}

	if depth > d.maxDepth {
		out := map[string]interface{}{"note": depthNote}
		if len(sch.Type) > 0 {
			out["type"] = joinTypes(sch.Type)
		}
		if ref != "" {
			out["$ref"] = ref
		}
		return out
	}

	childSeen := seen
	if ref != "" {
		childSeen = cloneSeen(seen, ref)
	}

	out := map[string]interface{}{}
	properties := map[string]interface{}{}
	var required []string
	reqSeen := map[string]bool{}
	addRequired := func(names []string) {
		for _, n := range names {
			if !reqSeen[n] {
				reqSeen[n] = true
				required = append(required, n)
			}
		}
	}

	// allOf composes at the same level. Object members contribute their
	// properties and required, merged here at the same depth since allOf is
	// composition rather than nesting. A member that instead carries a
	// non-object shape is the `.describe()`-on-a-ref pattern: a typed ref —
	// array, scalar, or union — wrapped in an allOf alongside a description-only
	// sibling. We adopt that shape directly (see allOfShapeKeys); without it the
	// field would lose its type/items and collapse to an empty object.
	for _, member := range sch.AllOf {
		sub, ok := d.simplify(member, depth, childSeen).(map[string]interface{})
		if !ok {
			continue
		}
		if props, ok := sub["properties"].(map[string]interface{}); ok {
			for k, v := range props {
				properties[k] = v
			}
		}
		if req, ok := sub["required"].([]string); ok {
			addRequired(req)
		}
		for _, k := range allOfShapeKeys {
			if v, ok := sub[k]; ok {
				out[k] = v
			}
		}
	}

	// This schema's own properties (nested one level deeper).
	if sch.Properties != nil {
		for pair := sch.Properties.First(); pair != nil; pair = pair.Next() {
			properties[pair.Key()] = d.simplify(pair.Value(), depth+1, childSeen)
		}
	}
	addRequired(sch.Required)

	switch {
	case len(sch.Type) > 0:
		out["type"] = joinTypes(sch.Type)
	case len(properties) > 0:
		out["type"] = "object"
	}
	if sch.Description != "" {
		out["description"] = sch.Description
	}
	if sch.Format != "" {
		out["format"] = sch.Format
	}
	if enum := decodeNodes(sch.Enum); len(enum) > 0 {
		out["enum"] = enum
	}
	if ex := exampleOf(sch); ex != nil {
		out["example"] = ex
	}
	if def := decodeNode(sch.Default); def != nil {
		out["default"] = def
	}
	if sch.Items != nil && sch.Items.IsA() {
		out["items"] = d.simplify(sch.Items.A, depth+1, childSeen)
	}
	// Map types carry their value shape in additionalProperties (a $ref or
	// inline schema) rather than properties — e.g. queryPresentations.data,
	// keyed by tab ID. Expand it so the value schema isn't dropped.
	if sch.AdditionalProperties != nil && sch.AdditionalProperties.IsA() {
		out["additionalProperties"] = d.simplify(sch.AdditionalProperties.A, depth+1, childSeen)
	}
	if len(sch.OneOf) > 0 {
		out["oneOf"] = d.simplifyList(sch.OneOf, depth+1, childSeen)
	}
	if len(sch.AnyOf) > 0 {
		out["anyOf"] = d.simplifyList(sch.AnyOf, depth+1, childSeen)
	}
	if len(properties) > 0 {
		out["properties"] = properties
	}
	if len(required) > 0 {
		out["required"] = required
	}

	return out
}

func (d *describer) simplifyList(proxies []*base.SchemaProxy, depth int, seen map[string]bool) []interface{} {
	out := make([]interface{}, 0, len(proxies))
	for _, p := range proxies {
		out = append(out, d.simplify(p, depth, seen))
	}
	return out
}

// synth builds a minimal, copy-pasteable example value for a schema:
// only required object fields are included, filled from explicit examples,
// defaults, enums, or a typed placeholder. `name` is the field name, used to
// make string placeholders self-describing (e.g. "<modelId>").
func (d *describer) synth(proxy *base.SchemaProxy, name string, depth int, seen map[string]bool) interface{} {
	if proxy == nil {
		return placeholder(name, "string")
	}

	ref := ""
	if proxy.IsReference() {
		ref = proxy.GetReference()
		if seen[ref] {
			return nil
		}
	}

	sch := proxy.Schema()
	if sch == nil {
		return placeholder(name, "string")
	}

	// Explicit example / default / enum win over a synthesized placeholder.
	if ex := exampleOf(sch); ex != nil {
		return ex
	}
	if def := decodeNode(sch.Default); def != nil {
		return def
	}
	if enum := decodeNodes(sch.Enum); len(enum) > 0 {
		return enum[0]
	}

	childSeen := seen
	if ref != "" {
		childSeen = cloneSeen(seen, ref)
	}

	t := firstType(sch)

	if t == "object" || sch.Properties != nil || len(sch.AllOf) > 0 {
		obj := map[string]interface{}{}
		if depth > d.maxDepth {
			return obj
		}
		reqSet, props := gatherObject(sch, childSeen)
		// A `.describe()`-on-a-ref wrapper around a non-object (e.g. an array or
		// scalar ref beside a description-only sibling) has nothing to gather as
		// an object. Synthesize the underlying typed member instead of {}.
		if len(props) == 0 && len(reqSet) == 0 && len(sch.AllOf) > 0 {
			if m := substantiveAllOfMember(sch); m != nil {
				return d.synth(m, name, depth, childSeen)
			}
		}
		for _, fieldName := range sortedKeys(reqSet) {
			if p, ok := props[fieldName]; ok {
				obj[fieldName] = d.synth(p, fieldName, depth+1, childSeen)
			} else {
				obj[fieldName] = placeholder(fieldName, "string")
			}
		}
		// Pure map type (additionalProperties, no fixed properties) — show one
		// representative entry so the agent sees the value shape.
		if len(props) == 0 && sch.AdditionalProperties != nil && sch.AdditionalProperties.IsA() {
			obj["<key>"] = d.synth(sch.AdditionalProperties.A, "", depth+1, childSeen)
		}
		return obj
	}

	if t == "array" {
		if sch.Items != nil && sch.Items.IsA() {
			return []interface{}{d.synth(sch.Items.A, singular(name), depth+1, childSeen)}
		}
		return []interface{}{}
	}

	return placeholder(name, t)
}

// gatherObject collects the required field names and property proxies for an
// object schema, flattening any allOf members. `seen` guards against recursive
// allOf composition.
func gatherObject(sch *base.Schema, seen map[string]bool) (map[string]bool, map[string]*base.SchemaProxy) {
	reqSet := map[string]bool{}
	props := map[string]*base.SchemaProxy{}

	for _, member := range sch.AllOf {
		if member == nil {
			continue
		}
		ref := ""
		if member.IsReference() {
			ref = member.GetReference()
			if seen[ref] {
				continue
			}
		}
		ms := member.Schema()
		if ms == nil {
			continue
		}
		memberSeen := seen
		if ref != "" {
			memberSeen = cloneSeen(seen, ref)
		}
		subReq, subProps := gatherObject(ms, memberSeen)
		for k := range subReq {
			reqSet[k] = true
		}
		for k, v := range subProps {
			props[k] = v
		}
	}

	if sch.Properties != nil {
		for pair := sch.Properties.First(); pair != nil; pair = pair.Next() {
			props[pair.Key()] = pair.Value()
		}
	}
	for _, r := range sch.Required {
		reqSet[r] = true
	}

	return reqSet, props
}

// substantiveAllOfMember returns the single allOf member that carries an actual
// shape — a $ref or any typed/composed schema — ignoring description-only
// siblings. It lets synth handle the `.describe()`-on-a-ref pattern, where a
// typed ref is wrapped in an allOf next to a metadata-only member. It returns
// nil unless exactly one member is substantive, so an ambiguous composition
// falls back to plain object handling.
func substantiveAllOfMember(sch *base.Schema) *base.SchemaProxy {
	var found *base.SchemaProxy
	for _, m := range sch.AllOf {
		if m == nil {
			continue
		}
		if !m.IsReference() {
			ms := m.Schema()
			if ms == nil || isMetadataOnly(ms) {
				continue
			}
		}
		if found != nil {
			return nil
		}
		found = m
	}
	return found
}

// isMetadataOnly reports whether a schema carries no structural shape — only
// annotations like description. Such a member is the description-only sibling in
// the `.describe()`-on-a-ref pattern.
func isMetadataOnly(sch *base.Schema) bool {
	return len(sch.Type) == 0 &&
		sch.Properties == nil &&
		len(sch.AllOf) == 0 &&
		len(sch.AnyOf) == 0 &&
		len(sch.OneOf) == 0 &&
		sch.Items == nil &&
		len(sch.Enum) == 0 &&
		(sch.AdditionalProperties == nil || !sch.AdditionalProperties.IsA())
}

// placeholder returns a typed stand-in value for a required field that carries
// no example. Strings echo the field name so the agent knows what to fill in.
func placeholder(name, typ string) interface{} {
	switch typ {
	case "integer", "number":
		return 0
	case "boolean":
		return false
	case "array":
		return []interface{}{}
	case "object":
		return map[string]interface{}{}
	default:
		if name == "" {
			return "<value>"
		}
		return "<" + name + ">"
	}
}

// exampleOf returns the decoded value of a schema's example (or first of its
// examples list), or nil if none is set.
func exampleOf(sch *base.Schema) interface{} {
	if sch.Example != nil {
		if v := decodeNode(sch.Example); v != nil {
			return v
		}
	}
	for _, e := range sch.Examples {
		if v := decodeNode(e); v != nil {
			return v
		}
	}
	return nil
}

// decodeNode converts a YAML node from the spec into a plain Go value.
func decodeNode(n *yaml.Node) interface{} {
	if n == nil {
		return nil
	}
	var v interface{}
	if err := n.Decode(&v); err != nil {
		return nil
	}
	return v
}

func decodeNodes(nodes []*yaml.Node) []interface{} {
	var out []interface{}
	for _, n := range nodes {
		if v := decodeNode(n); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func firstType(sch *base.Schema) string {
	if len(sch.Type) > 0 {
		return sch.Type[0]
	}
	return ""
}

// joinTypes renders a type list. OpenAPI 3.1 allows unions like
// ["string","null"]; we surface them as "string | null".
func joinTypes(types []string) interface{} {
	if len(types) == 1 {
		return types[0]
	}
	return strings.Join(types, " | ")
}

func cloneSeen(seen map[string]bool, add string) map[string]bool {
	next := make(map[string]bool, len(seen)+1)
	for k := range seen {
		next[k] = true
	}
	next[add] = true
	return next
}

// singular strips a trailing plural "s" so an array named "users" yields an
// item example labeled "user". Best-effort; cosmetic only.
func singular(name string) string {
	if len(name) > 1 && strings.HasSuffix(name, "s") {
		return name[:len(name)-1]
	}
	return name
}
