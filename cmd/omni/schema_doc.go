package main

// Helpers shared by the hand-written API commands' static --schema documents
// (see createBranchSchemaDoc and setAttributesSchemaDoc). They keep those
// documents in the same plain-Go shape the spec describer emits.

// schemaString is a string-typed schema node carrying a description — by far the
// most common field shape in the static documents.
func schemaString(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}
