package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/exploreomni/omni-cli/internal/auth"
	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/openapi"
	"github.com/spf13/cobra"
)

// userAttributePrefix is the key under which Omni user attributes live in the
// SCIM user representation. In a SCIM PATCH operation it is the prefix of the
// dotted sub-attribute path (e.g. "urn:omni:params:1.0:UserAttribute.region").
const userAttributePrefix = "urn:omni:params:1.0:UserAttribute"

// scimPatchOp is a single SCIM PatchOp operation. Value has no omitempty so a
// nil value marshals to "value": null (clearing the attribute) — the schema
// requires the value field on every operation.
type scimPatchOp struct {
	Op    string      `json:"op"`
	Path  string      `json:"path,omitempty"`
	Value interface{} `json:"value"`
}

type scimPatchBody struct {
	Schemas    []string      `json:"schemas"`
	Operations []scimPatchOp `json:"Operations"`
}

// addUserCommands attaches hand-written user convenience commands to the
// generated "users" command group. No-op if the group is absent.
func addUserCommands(root *cobra.Command, exec openapi.Executor) {
	var usersCmd *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "users" {
			usersCmd = cmd
			break
		}
	}
	if usersCmd == nil {
		return
	}
	usersCmd.AddCommand(setUserAttributesCmd(exec))
}

func setUserAttributesCmd(exec openapi.Executor) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-attributes <user-id-or-email>",
		Short: "Set Omni user attribute values on an existing user",
		Long: `Set Omni user attribute values on an existing user.

The user can be given as either a SCIM user ID (the UUID shown by
"omni scim users list") or an email address. An email address (any value
containing "@") is transparently resolved to the user's ID via a SCIM lookup.

Attributes are applied via SCIM PATCH, so only the attributes you name are
changed; the user's other attributes are left untouched. Values given with
--attr are sent as strings; an empty value (--attr region=) clears that
attribute. Use --attr-json to set numeric or multi-value (array) attributes.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			attrs, _ := cmd.Flags().GetStringArray("attr")
			attrJSON, _ := cmd.Flags().GetString("attr-json")

			body, err := buildSetAttributesBody(attrs, attrJSON)
			if err != nil {
				return err
			}

			userID := args[0]
			if isEmail(userID) {
				cfg, err := resolveConfig(cmd)
				if err != nil {
					return err
				}
				userID, err = lookupUserIDByEmail(cfg, userID)
				if err != nil {
					return err
				}
			}

			return exec(openapi.APIRequest{
				Cmd:    cmd,
				Method: "PATCH",
				Path:   "/api/scim/v2/Users/" + url.PathEscape(userID),
				Body:   body,
			})
		},
	}

	cmd.Flags().StringArray("attr", nil, "user attribute as key=value (repeatable); empty value clears the attribute")
	cmd.Flags().String("attr-json", "", `JSON object of attributes for numeric/array values, e.g. '{"level":3,"regions":["us","eu"]}'`)

	// Hand-written, but agents reach for --schema on any API command; describe the
	// SCIM PATCH body it assembles so the flag works here too.
	openapi.RegisterSchemaFlag(cmd, openapi.StaticSchemaEmitter("set-attributes", setAttributesSchemaDoc))

	cmd.Example = `  # Set two string attributes (by user ID)
  omni users set-attributes 550e8400-e29b-41d4-a716-446655440000 --attr region=us-east --attr team=growth

  # Identify the user by email instead of ID
  omni users set-attributes user@example.com --attr region=us-east

  # Clear an attribute
  omni users set-attributes user@example.com --attr region=

  # Set numeric / multi-value attributes
  omni users set-attributes user@example.com --attr-json '{"level":3,"regions":["us","eu"]}'`

	return cmd
}

// setAttributesSchemaDoc mirrors the generated commands' --schema document for
// the hand-written set-attributes command. The body shown is the SCIM PatchOp
// the CLI assembles from --attr/--attr-json; the response is the SCIM user
// representation returned by PATCH /api/scim/v2/Users/{id}.
// Hand-transcribed, so TestStaticSchemaDocs_ResponseMatchesSpec checks it
// against the spec and the body-assembly code — keep both in step.
func setAttributesSchemaDoc() openapi.SchemaDoc {
	return openapi.SchemaDoc{
		Method: "PATCH",
		Path:   "/api/scim/v2/Users/{id}",
		Args: []openapi.SchemaArg{{
			Name:        "userIdOrEmail",
			Placeholder: "<user-id-or-email>",
			Type:        "string",
			Description: "SCIM user ID, or an email address resolved to one via a SCIM lookup",
		}},
		Required: []string{"schemas", "Operations"},
		Body: map[string]interface{}{
			"type":        "object",
			"description": "SCIM PatchOp assembled by the CLI from --attr/--attr-json; not passed through as JSON.",
			"properties": map[string]interface{}{
				"schemas": map[string]interface{}{
					"type":        "array",
					"description": `always ["urn:ietf:params:scim:api:messages:2.0:PatchOp"]`,
					"items":       map[string]interface{}{"type": "string"},
				},
				"Operations": map[string]interface{}{
					"type":        "array",
					"description": "a single replace op nesting the attributes under " + userAttributePrefix,
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"op":    schemaString(`always "replace"`),
							"value": map[string]interface{}{"type": "object", "description": "attribute name → value; null clears the attribute"},
						},
						"required": []string{"op", "value"},
					},
				},
			},
			"required": []string{"schemas", "Operations"},
		},
		Example: map[string]interface{}{
			"schemas": []interface{}{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
			"Operations": []interface{}{map[string]interface{}{
				"op": "replace",
				"value": map[string]interface{}{
					userAttributePrefix: map[string]interface{}{"region": "us-east"},
				},
			}},
		},
		Response: &openapi.SchemaResponse{
			Status:      "200",
			ContentType: "application/json",
			Description: "SCIM user updated",
			Schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"active":      map[string]interface{}{"type": "boolean", "description": "Whether the user is active"},
					"displayName": schemaString("Display name"),
					"id":          map[string]interface{}{"type": "string", "format": "uuid", "description": "SCIM user ID"},
					"schemas":     map[string]interface{}{"type": "array", "description": "SCIM schema URIs", "items": map[string]interface{}{"type": "string"}},
					"userName":    map[string]interface{}{"type": "string", "format": "email", "description": "Username (email)"},
				},
				"required": []string{"active", "displayName", "id", "schemas", "userName"},
			},
		},
	}
}

// buildSetAttributesBody assembles a SCIM PatchOp body that sets Omni user
// attributes. It uses the "no path" namespaced value-object form (the Okta /
// RFC 7644 §3.5.2 format the Omni SCIM endpoint expects): a single replace
// operation whose value nests the attributes under the Omni attribute
// namespace. The server merges these over the user's existing attributes, so
// only the named attributes change; an empty --attr value sets the attribute to
// null, clearing it.
func buildSetAttributesBody(attrs []string, attrJSON string) ([]byte, error) {
	values := map[string]interface{}{}
	seen := map[string]bool{}

	add := func(key string, v interface{}) error {
		if key == "" {
			return fmt.Errorf("empty attribute name")
		}
		if seen[key] {
			return fmt.Errorf("attribute %q specified more than once", key)
		}
		seen[key] = true
		values[key] = v
		return nil
	}

	for _, a := range attrs {
		key, val, found := strings.Cut(a, "=")
		if !found {
			return nil, fmt.Errorf("invalid --attr %q: expected key=value", a)
		}
		var v interface{} // nil → JSON null, which clears the attribute
		if val != "" {
			v = val
		}
		if err := add(strings.TrimSpace(key), v); err != nil {
			return nil, err
		}
	}

	if strings.TrimSpace(attrJSON) != "" {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(attrJSON), &m); err != nil {
			return nil, fmt.Errorf("invalid --attr-json: %w", err)
		}
		for k, raw := range m {
			var v interface{}
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, fmt.Errorf("invalid --attr-json value for %q: %w", k, err)
			}
			if err := add(strings.TrimSpace(k), v); err != nil {
				return nil, err
			}
		}
	}

	if len(values) == 0 {
		return nil, fmt.Errorf("provide at least one --attr or --attr-json")
	}

	return json.Marshal(scimPatchBody{
		Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		Operations: []scimPatchOp{{
			Op:    "replace",
			Value: map[string]interface{}{userAttributePrefix: values},
		}},
	})
}

// isEmail reports whether s should be treated as an email address rather than a
// user ID. SCIM/user IDs are UUIDs and never contain "@", so its presence is a
// reliable discriminator.
func isEmail(s string) bool {
	return strings.Contains(s, "@")
}

// lookupUserIDByEmail resolves an email address to its SCIM user ID via the
// SCIM Users list filtered by userName. It errors unless exactly one user
// matches, so an ambiguous result never silently targets the wrong user.
func lookupUserIDByEmail(cfg *config.ResolvedConfig, email string) (string, error) {
	q := url.Values{}
	q.Set("filter", fmt.Sprintf("userName eq %q", email))

	resp, err := auth.Do(cfg, "GET", "/api/scim/v2/Users?"+q.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("looking up user by email: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading user lookup response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("looking up user by email: %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var list struct {
		Resources []struct {
			ID       string `json:"id"`
			UserName string `json:"userName"`
		} `json:"Resources"`
	}
	if err := json.Unmarshal(respBody, &list); err != nil {
		return "", fmt.Errorf("parsing user lookup response: %w", err)
	}

	// Verify the userName matches exactly (case-insensitive) rather than trusting
	// the server filter blindly, so a lenient filter can't target the wrong user.
	var ids []string
	for _, r := range list.Resources {
		if strings.EqualFold(r.UserName, email) && r.ID != "" {
			ids = append(ids, r.ID)
		}
	}

	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no user found with email %q", email)
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("multiple users found with email %q; specify the user ID instead", email)
	}
}
