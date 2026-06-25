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
// request body. Some bodies (notably the v2 document content blob) nest very
// deeply; without a cap a single --schema dump could be megabytes. Beyond this
// depth we emit a short placeholder noting the omission rather than recursing.
const maxSchemaDepth = 8

// bodySchemaDoc is the JSON document emitted by `omni <cmd> --schema`. It gives
// an agent both the authoritative contract (Body) and a copy-pasteable starting
// point (Example) for an operation's request body.
type bodySchemaDoc struct {
	Method   string      `json:"method"`
	Path     string      `json:"path"`
	Required []string    `json:"required,omitempty"`
	Body     interface{} `json:"body"`
	Example  interface{} `json:"example,omitempty"`
}

// emitBodySchema writes the resolved request-body schema and a synthesized
// example to the command's stdout, honoring the global --compact flag. It makes
// no network call and needs no auth.
func emitBodySchema(cmd *cobra.Command, op *operationInfo) error {
	doc := describeBody(op)

	compact, _ := cmd.Flags().GetBool("compact")
	var data []byte
	var err error
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

// describeBody builds the schema document for an operation's request body.
func describeBody(op *operationInfo) bodySchemaDoc {
	doc := bodySchemaDoc{Method: op.Method, Path: op.Path}
	if op.BodySchema == nil {
		return doc
	}

	body := simplifySchema(op.BodySchema, 0, nil)
	doc.Body = body
	if m, ok := body.(map[string]interface{}); ok {
		if req, ok := m["required"].([]string); ok {
			doc.Required = req
		}
	}
	doc.Example = synthExample(op.BodySchema, "", 0, nil)
	return doc
}

// simplifySchema turns a libopenapi schema into a compact, agent-friendly map.
// It merges allOf composition into a single object, preserves descriptions,
// enums, formats, required fields and examples, and guards against deep nesting
// and recursive $refs. `seen` tracks the $refs already expanded on the current
// path so a self-referential schema (e.g. folder → children → folder) stops
// instead of looping forever.
func simplifySchema(proxy *base.SchemaProxy, depth int, seen map[string]bool) interface{} {
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

	if depth > maxSchemaDepth {
		out := map[string]interface{}{"note": "max depth reached; expansion omitted"}
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

	// allOf composes at the same level: merge member properties and required
	// into this object. Members are expanded at the same depth, since allOf is
	// composition rather than nesting.
	for _, member := range sch.AllOf {
		sub, ok := simplifySchema(member, depth, childSeen).(map[string]interface{})
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
	}

	// This schema's own properties (nested one level deeper).
	if sch.Properties != nil {
		for pair := sch.Properties.First(); pair != nil; pair = pair.Next() {
			properties[pair.Key()] = simplifySchema(pair.Value(), depth+1, childSeen)
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
		out["items"] = simplifySchema(sch.Items.A, depth+1, childSeen)
	}
	if len(sch.OneOf) > 0 {
		out["oneOf"] = simplifyList(sch.OneOf, depth+1, childSeen)
	}
	if len(sch.AnyOf) > 0 {
		out["anyOf"] = simplifyList(sch.AnyOf, depth+1, childSeen)
	}
	if len(properties) > 0 {
		out["properties"] = properties
	}
	if len(required) > 0 {
		out["required"] = required
	}

	return out
}

func simplifyList(proxies []*base.SchemaProxy, depth int, seen map[string]bool) []interface{} {
	out := make([]interface{}, 0, len(proxies))
	for _, p := range proxies {
		out = append(out, simplifySchema(p, depth, seen))
	}
	return out
}

// synthExample builds a minimal, copy-pasteable example value for a schema:
// only required object fields are included, filled from explicit examples,
// defaults, enums, or a typed placeholder. `name` is the field name, used to
// make string placeholders self-describing (e.g. "<modelId>").
func synthExample(proxy *base.SchemaProxy, name string, depth int, seen map[string]bool) interface{} {
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
		if depth > maxSchemaDepth {
			return obj
		}
		reqSet, props := gatherObject(sch, childSeen)
		for _, fieldName := range sortedKeys(reqSet) {
			if p, ok := props[fieldName]; ok {
				obj[fieldName] = synthExample(p, fieldName, depth+1, childSeen)
			} else {
				obj[fieldName] = placeholder(fieldName, "string")
			}
		}
		return obj
	}

	if t == "array" {
		if sch.Items != nil && sch.Items.IsA() {
			return []interface{}{synthExample(sch.Items.A, singular(name), depth+1, childSeen)}
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
