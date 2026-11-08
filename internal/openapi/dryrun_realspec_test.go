package openapi

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
)

// The cost --dry-run must never impose is blocking a valid request. The best
// valid requests we have offline are the example bodies --schema synthesizes
// from the spec itself: every one of them has to pass, so a spec sync that adds
// a shape the validator misreads fails here instead of in a user's terminal.
func TestRealSpec_SchemaExamplesPassDryRun(t *testing.T) {
	ops := realSpecOperations(t)
	checked := 0
	for _, op := range ops {
		if !op.HasBody || op.BodyMediaType == "multipart/form-data" || !op.bodyFlagIsJSON() {
			continue
		}
		doc, err := describeBody(op, "", maxSchemaDepth, SchemaFlags{Schema: "schema", Field: "field", Depth: "depth"})
		if err != nil || doc.Example == nil {
			continue
		}
		if containsDepthPlaceholder(doc.Example) {
			continue // truncated example, not a faithful body
		}
		body, err := json.Marshal(doc.Example)
		if err != nil {
			t.Fatalf("%s: marshal example: %v", op.OperationID, err)
		}
		checked++
		for _, v := range ValidateBody(op.BodySchema, body) {
			if placeholderViolation(v) {
				continue
			}
			t.Errorf("%s: --dry-run rejects the spec's own example %s: %s: %s", op.OperationID, body, v.Path, v.Message)
		}
	}
	if checked < 20 {
		t.Fatalf("only %d example bodies checked; expected the real spec to provide many", checked)
	}
	t.Logf("checked %d example bodies", checked)
}

// realSpecOperations returns every operation in the embedded spec, parsed the
// way GenerateCommands parses it.
func realSpecOperations(t *testing.T) []*operationInfo {
	t.Helper()
	doc, err := libopenapi.NewDocument(loadSpec(t))
	if err != nil {
		t.Fatalf("parsing spec: %v", err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("building model: %v", err)
	}
	groups := map[string][]*operationInfo{}
	for pair := model.Model.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		extractOperations(pair.Key(), pair.Value(), groups)
	}
	var ops []*operationInfo
	for _, tag := range sortedKeys(groups) {
		ops = append(ops, groups[tag]...)
	}
	return ops
}

func containsDepthPlaceholder(v interface{}) bool {
	switch val := v.(type) {
	case map[string]interface{}:
		if val["note"] == depthNote {
			return true
		}
		for _, x := range val {
			if containsDepthPlaceholder(x) {
				return true
			}
		}
	case []interface{}:
		for _, x := range val {
			if containsDepthPlaceholder(x) {
				return true
			}
		}
	}
	return false
}

// placeholderViolation reports the violations an example body is expected to
// have: --schema fills fields it has no example for with "<name>" placeholders
// and empty arrays, which are templates for the caller to fill in, not values.
// The structural checks (unknown keys, required fields, types, enums) still
// apply in full.
func placeholderViolation(v Violation) bool {
	for _, p := range []string{"must be a UUID, got \"<", "must match pattern", "must have at least", "must be at least"} {
		if strings.HasPrefix(v.Message, p) {
			return true
		}
	}
	return false
}

// The shorthand help examples are copied by agents too. Their IDs are
// abbreviated ("770e8400-..."), so only UUID format is excused; keys,
// required fields, types and enums must all hold.
func TestRealSpec_ShorthandExamplesPassDryRun(t *testing.T) {
	bodyArg := regexp.MustCompile(`--body '([^']+)'`)
	byID := map[string]*operationInfo{}
	for _, op := range realSpecOperations(t) {
		byID[op.OperationID] = op
	}
	checked := 0
	for _, id := range sortedKeys(bodyShorthands) {
		m := bodyArg.FindStringSubmatch(bodyShorthands[id].ExampleJSON)
		if m == nil {
			continue
		}
		op := byID[id]
		if op == nil {
			t.Errorf("shorthand %s has no operation in the spec", id)
			continue
		}
		checked++
		for _, v := range ValidateBody(op.BodySchema, []byte(m[1])) {
			if strings.HasPrefix(v.Message, "must be a UUID") {
				continue
			}
			t.Errorf("%s: example body %s: %s: %s", id, m[1], v.Path, v.Message)
		}
	}
	if checked == 0 {
		t.Fatal("no shorthand examples checked")
	}
}
