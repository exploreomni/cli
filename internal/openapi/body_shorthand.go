package openapi

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/spf13/cobra"
)

// ArgMapping describes how a positional arg maps to a JSON body field.
type ArgMapping struct {
	Name        string // display name for <arg> in usage
	FieldPath   string // JSON field name
	Description string
	Transform   string // "string", "uuid", "email-list"
}

// FlagMapping describes an optional body field exposed as a CLI flag.
type FlagMapping struct {
	FlagName    string // CLI flag name
	FieldPath   string // JSON field name
	Description string
	Default     string
}

// shorthandFlagValue keeps promoted body flags value-taking (so both
// `--flag true` and `--flag false` work) while exposing the OpenAPI type in
// Cobra's help output. The raw token is converted when the JSON body is built.
type shorthandFlagValue struct {
	value    string
	typeName string
}

func (v *shorthandFlagValue) Set(value string) error {
	v.value = value
	return nil
}

func (v *shorthandFlagValue) String() string {
	if v == nil {
		return ""
	}
	return v.value
}

func (v *shorthandFlagValue) Type() string {
	if v.typeName == "" {
		return "string"
	}
	return v.typeName
}

// BodyShorthand defines how a single operation's body can be simplified.
type BodyShorthand struct {
	OperationID  string
	Args         []ArgMapping
	Flags        []FlagMapping
	ExampleShort string
	ExampleJSON  string
}

// bodyShorthands maps operationId to its shorthand definition.
var bodyShorthands = map[string]*BodyShorthand{
	// Tier 1: AI commands
	"aiSearchOmniDocs": {
		Args: []ArgMapping{
			{Name: "question", FieldPath: "question", Description: "natural language question about Omni", Transform: "string"},
		},
		ExampleShort: `omni ai search-omni-docs "How do I add a format to a dimension?"`,
		ExampleJSON:  `omni ai search-omni-docs --body '{"question":"How do I add a format to a dimension?"}'`,
	},
	"aiGenerateQuery": {
		Args: []ArgMapping{
			{Name: "model-id", FieldPath: "modelId", Description: "UUID of the shared model", Transform: "string"},
			{Name: "prompt", FieldPath: "prompt", Description: "natural language query prompt", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "run-query", FieldPath: "runQuery", Description: "execute the generated query (server default: true)"},
			{FlagName: "user-id", FieldPath: "userId", Description: "user ID to execute as"},
			{FlagName: "workbook-url", FieldPath: "workbookUrl", Description: "create a workbook with the generated query and return its URL"},
			{FlagName: "current-topic-name", FieldPath: "currentTopicName", Description: "topic name to scope query generation"},
			{FlagName: "branch-id", FieldPath: "branchId", Description: "branch ID for the model"},
		},
		ExampleShort: `omni ai generate-query 770e8400-e29b-41d4-a716-446655440002 "Show total revenue by month"`,
		ExampleJSON:  `omni ai generate-query --body '{"modelId":"770e8400-...","prompt":"Show total revenue by month"}'`,
	},
	"aiPickTopic": {
		Args: []ArgMapping{
			{Name: "model-id", FieldPath: "modelId", Description: "UUID of the shared model", Transform: "string"},
			{Name: "prompt", FieldPath: "prompt", Description: "natural language prompt", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "user-id", FieldPath: "userId", Description: "user ID to execute as"},
			{FlagName: "branch-id", FieldPath: "branchId", Description: "branch ID for the model"},
		},
		ExampleShort: `omni ai pick-topic 770e8400-e29b-41d4-a716-446655440002 "How many orders last month?"`,
		ExampleJSON:  `omni ai pick-topic --body '{"modelId":"770e8400-...","prompt":"How many orders last month?"}'`,
	},
	"aiJobSubmit": {
		Args: []ArgMapping{
			{Name: "model-id", FieldPath: "modelId", Description: "UUID of the shared model", Transform: "string"},
			{Name: "prompt", FieldPath: "prompt", Description: "natural language prompt", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "topic-name", FieldPath: "topicName", Description: "topic name to scope the query"},
			{FlagName: "branch-id", FieldPath: "branchId", Description: "branch ID for the model"},
			{FlagName: "conversation-id", FieldPath: "conversationId", Description: "conversation ID to continue"},
			{FlagName: "webhook-url", FieldPath: "webhookUrl", Description: "webhook URL for job completion"},
			{FlagName: "webhook-signing-secret", FieldPath: "webhookSigningSecret", Description: "webhook signing secret"},
			{FlagName: "progress-webhook-enabled", FieldPath: "progressWebhookEnabled", Description: "enable progress webhooks"},
		},
		ExampleShort: `omni ai job-submit 770e8400-e29b-41d4-a716-446655440002 "Top 5 products by revenue"`,
		ExampleJSON:  `omni ai job-submit --body '{"modelId":"770e8400-...","prompt":"Top 5 products by revenue"}'`,
	},

	// Tier 2: User commands
	"usersCreateEmailOnly": {
		Args: []ArgMapping{
			{Name: "email", FieldPath: "email", Description: "email address for the new user", Transform: "string"},
		},
		ExampleShort: `omni users create-email-only user@example.com`,
		ExampleJSON:  `omni users create-email-only --body '{"email":"user@example.com"}'`,
	},
	"usersCreateEmailOnlyBulk": {
		Args: []ArgMapping{
			{Name: "emails", FieldPath: "users", Description: "comma-separated list of email addresses", Transform: "email-list"},
		},
		ExampleShort: `omni users create-email-only-bulk "a@co.com,b@co.com,c@co.com"`,
		ExampleJSON:  `omni users create-email-only-bulk --body '{"users":[{"email":"a@co.com"},{"email":"b@co.com"}]}'`,
	},

	// Tier 3: Single required field → positional arg
	"documentsMove": {
		Args: []ArgMapping{
			{Name: "folder-path", FieldPath: "folderPath", Description: "destination folder path (use \"null\" for root)", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "scope", FieldPath: "scope", Description: "access scope: restricted or organization"},
		},
		ExampleShort: `omni documents move <identifier> "/my/folder"`,
		ExampleJSON:  `omni documents move <identifier> --body '{"folderPath":"/my/folder","scope":"organization"}'`,
	},
	"documentsDuplicate": {
		Args: []ArgMapping{
			{Name: "name", FieldPath: "name", Description: "name for the duplicated document", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "folder-path", FieldPath: "folderPath", Description: "destination folder path"},
			{FlagName: "scope", FieldPath: "scope", Description: "access scope: restricted or organization"},
		},
		ExampleShort: `omni documents duplicate <identifier> "Copy of Dashboard"`,
		ExampleJSON:  `omni documents duplicate <identifier> --body '{"name":"Copy of Dashboard"}'`,
	},
	"documentsTransferOwnership": {
		Args: []ArgMapping{
			{Name: "user-id", FieldPath: "userId", Description: "membership ID of the new owner", Transform: "string"},
		},
		ExampleShort: `omni documents transfer-ownership <identifier> 987fcdeb-51a2-43d7-9b56-254415f67890`,
		ExampleJSON:  `omni documents transfer-ownership <identifier> --body '{"userId":"987fcdeb-..."}'`,
	},
	"schedulesTransferOwnership": {
		Args: []ArgMapping{
			{Name: "user-id", FieldPath: "userId", Description: "UUID of the new owner", Transform: "string"},
		},
		ExampleShort: `omni schedules transfer-ownership <schedule-id> 987fcdeb-51a2-43d7-9b56-254415f67890`,
		ExampleJSON:  `omni schedules transfer-ownership <schedule-id> --body '{"userId":"987fcdeb-..."}'`,
	},
	"modelsMigrate": {
		Args: []ArgMapping{
			{Name: "target-model-id", FieldPath: "targetModelId", Description: "target model ID to migrate to", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "branch-name", FieldPath: "branchName", Description: "branch name for the target model"},
			{FlagName: "commit-message", FieldPath: "commitMessage", Description: "commit message for git sync"},
			{FlagName: "git-ref", FieldPath: "gitRef", Description: "git reference"},
		},
		ExampleShort: `omni models migrate <model-id> <target-model-id>`,
		ExampleJSON:  `omni models migrate <model-id> --body '{"targetModelId":"..."}'`,
	},
	"modelsBranchDbt": {
		Args: []ArgMapping{
			{Name: "dbt-environment-id", FieldPath: "dbt_environment_id", Description: "ID of the dbt environment", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "dbt-git-branch", FieldPath: "dbt_git_branch", Description: "git branch for the dbt environment"},
		},
		ExampleShort: `omni models branch-dbt <model-id> <branch-name> 123e4567-e89b-12d3-a456-426614174000`,
		ExampleJSON:  `omni models branch-dbt <model-id> <branch-name> --body '{"dbt_environment_id":"123e4567-..."}'`,
	},
	"labelsCreate": {
		Args: []ArgMapping{
			{Name: "name", FieldPath: "name", Description: "label name", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "color", FieldPath: "color", Description: "hex color (e.g. #0366d6)"},
			{FlagName: "description", FieldPath: "description", Description: "label description"},
		},
		ExampleShort: `omni labels create "important"`,
		ExampleJSON:  `omni labels create --body '{"name":"important","color":"#0366d6"}'`,
	},
	"foldersCreate": {
		Args: []ArgMapping{
			{Name: "name", FieldPath: "name", Description: "folder name", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "parent-folder-id", FieldPath: "parentFolderId", Description: "parent folder ID (omit for root)"},
			{FlagName: "scope", FieldPath: "scope", Description: "share scope: organization or restricted"},
		},
		ExampleShort: `omni folders create "My New Folder"`,
		ExampleJSON:  `omni folders create --body '{"name":"My New Folder"}'`,
	},

	// Tier 4: Flags-only promotion (no new positional args)
	"documentsUpdate": {
		Flags: []FlagMapping{
			{FlagName: "name", FieldPath: "name", Description: "new document name"},
			{FlagName: "description", FieldPath: "description", Description: "document description"},
			{FlagName: "clear-existing-draft", FieldPath: "clearExistingDraft", Description: "clear existing draft before updating"},
		},
		ExampleShort: `omni documents update <identifier> --name "New Name"`,
		ExampleJSON:  `omni documents update <identifier> --body '{"name":"New Name"}'`,
	},
	"modelsGitSync": {
		Flags: []FlagMapping{
			{FlagName: "commit-message", FieldPath: "commitMessage", Description: "commit message for the git sync"},
		},
		ExampleShort: `omni models git-sync <model-id> --commit-message "Update schema"`,
		ExampleJSON:  `omni models git-sync <model-id> --body '{"commitMessage":"Update schema"}'`,
	},

	// v2 documents. Metadata fields are promoted to flags; the heavy nested
	// content (containers / controls / queryPresentations / settings) stays
	// on --body/stdin.
	//
	// A v2-get response is a starting point for a draft PATCH body, NOT a
	// guaranteed clean round-trip: the response carries read-only/derived keys
	// the PATCH validator rejects (e.g. `hidden` on filter configs, and
	// per-tile keys such as calculations / column_totals / fill_fields /
	// pivots / row_totals / userEditedSQL / chartType / visType / fields /
	// config), so expect to strip fields until the PATCH is accepted. Tiles
	// (queryPresentations.data.<n>) and controls (controls.data.<id>) are
	// replace-whole — send the complete object, never a partial — and
	// queryPresentations.order must enumerate every presentation key. A 200 is
	// not proof the content survived; re-read with v2-get and check visConfig.
	"documentsV2Create": {
		Args: []ArgMapping{
			{Name: "model-id", FieldPath: "modelId", Description: "UUID of the model the document is built on", Transform: "string"},
			{Name: "name", FieldPath: "name", Description: "name for the new document", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "identifier", FieldPath: "identifier", Description: "identifier for the new document (server-minted if omitted)"},
			{FlagName: "description", FieldPath: "description", Description: "document description"},
			{FlagName: "folder-id", FieldPath: "folderId", Description: "destination folder ID (omit for the root folder)"},
		},
		ExampleShort: `omni documents v2-create 770e8400-e29b-41d4-a716-446655440002 "Q3 Revenue"`,
		ExampleJSON:  `omni documents v2-create --body '{"modelId":"770e8400-...","name":"Q3 Revenue"}'`,
	},
	"documentsV2PatchDraft": {
		Flags: []FlagMapping{
			{FlagName: "name", FieldPath: "name", Description: "document name"},
			{FlagName: "description", FieldPath: "description", Description: "document description"},
			{FlagName: "summary", FieldPath: "summary", Description: "what this patch changes; written to the history audit trail"},
			{FlagName: "branch-id", FieldPath: "branchId", Description: "branch the new draft is created on (omit for the main workspace)"},
		},
		ExampleShort: `omni documents v2-patch-draft <identifier> --name "WIP title"`,
		ExampleJSON:  `omni documents v2-patch-draft <identifier> --body '{"name":"WIP title"}'`,
	},
	"documentsV2PatchDraftByIdentifier": {
		Flags: []FlagMapping{
			{FlagName: "name", FieldPath: "name", Description: "document name"},
			{FlagName: "description", FieldPath: "description", Description: "document description"},
			{FlagName: "summary", FieldPath: "summary", Description: "what this patch changes; written to the history audit trail"},
		},
		ExampleShort: `omni documents v2-patch-draft-by-identifier <identifier> <draft-identifier> --name "Edited"`,
		ExampleJSON:  `omni documents v2-patch-draft-by-identifier <identifier> <draft-identifier> --body '{"name":"Edited"}'`,
	},
}

// bodyExclusiveSuffix is appended to every promoted flag's help text. Promoted
// flags assemble a JSON body, so they are rejected at runtime when --body /
// --json-body is also supplied (see applyBodyShorthand).
const bodyExclusiveSuffix = " (cannot be combined with --body)"

// GetBodyShorthand returns the shorthand for an operation, or nil if none exists.
func GetBodyShorthand(operationID string) *BodyShorthand {
	return bodyShorthands[operationID]
}

// applyBodyShorthand modifies a cobra command to support shorthand positional args
// and promoted flags as alternatives to --body.
func applyBodyShorthand(cmd *cobra.Command, op *operationInfo, sh *BodyShorthand) {
	numPathParams := len(op.PathParams)

	// Extend the Use string with shorthand arg placeholders
	for _, a := range sh.Args {
		cmd.Use += " <" + a.Name + ">"
	}

	// Keep promoted flags value-taking so callers can use an explicit value such
	// as `--run-query false`, but show their schema type in help. assembleBody
	// uses the same request schema to encode the JSON value. The --body
	// exclusivity note is appended here, for every flag regardless of type,
	// rather than baked into each Description literal so the help text can't
	// drift from the runtime check below.
	//
	// FlagName is the canonical kebab-case spelling. Registration goes through
	// the flag set's normalization function (installed in buildCommand), so
	// --branchid / --branchId / --branch_id reach the same flag, as do the
	// Lookup and Changed calls below.
	for _, f := range sh.Flags {
		fieldType, err := shorthandFieldType(op.BodySchema, f.FieldPath)
		if err != nil {
			fieldType = "string"
		}
		cmd.Flags().Var(&shorthandFlagValue{value: f.Default, typeName: fieldType}, f.FlagName, f.Description+bodyExclusiveSuffix)
	}

	// Say the same thing from the other side: --body's own description on a
	// shorthand-bearing command warns that promoted flags can't be mixed in.
	if len(sh.Flags) > 0 {
		if bodyFlag := cmd.Flags().Lookup("body"); bodyFlag != nil {
			bodyFlag.Usage += "; cannot be combined with this command's promoted field flags — put those fields in the JSON instead"
		}
	}

	// Replace the Args validator with a flexible one
	cmd.Args = flexibleArgs(numPathParams, len(sh.Args))

	// Wrap the original RunE to assemble body from shorthand args
	originalRunE := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		bodyFlag, _ := cmd.Flags().GetString("body")
		jsonBodyFlag, _ := cmd.Flags().GetString("json-body")

		rawBody := bodyFlag
		if jsonBodyFlag != "" {
			rawBody = jsonBodyFlag
		}

		// If --body/--json-body is provided, use existing behavior — but
		// reject explicitly-set shorthand flags rather than silently
		// dropping them from the request.
		if rawBody != "" {
			var conflicting []string
			for _, f := range sh.Flags {
				if cmd.Flags().Changed(f.FlagName) {
					conflicting = append(conflicting, "--"+f.FlagName)
				}
			}
			if len(conflicting) > 0 {
				return fmt.Errorf("%s cannot be combined with --body; include the field(s) in the JSON body instead", strings.Join(conflicting, ", "))
			}
			return originalRunE(cmd, args)
		}

		// Assemble body from shorthand args and promoted flags
		body, err := assembleBody(sh, args, numPathParams, cmd, op.BodySchema)
		if err != nil {
			return err
		}

		// Set the body flag so the original RunE picks it up
		if err := cmd.Flags().Set("body", string(body)); err != nil {
			return err
		}

		return originalRunE(cmd, args[:numPathParams])
	}

	// Set examples using cobra's dedicated Example field
	cmd.Example = fmt.Sprintf("  # Shorthand\n  %s\n\n  # Equivalent JSON body\n  %s",
		sh.ExampleShort, sh.ExampleJSON)
}

// flexibleArgs returns a positional args validator that accepts either:
// - numPathParams args (when --body or --json-body is provided)
// - numPathParams + numShorthandArgs args (when using shorthand mode)
func flexibleArgs(numPathParams, numShorthandArgs int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		bodyFlag, _ := cmd.Flags().GetString("body")
		jsonBodyFlag, _ := cmd.Flags().GetString("json-body")

		if bodyFlag != "" || jsonBodyFlag != "" {
			if len(args) != numPathParams {
				return fmt.Errorf("accepts %d arg(s) when --body is used, received %d", numPathParams, len(args))
			}
			return nil
		}

		expected := numPathParams + numShorthandArgs
		if numShorthandArgs == 0 {
			// Flags-only shorthand: same arg count as path params
			if len(args) != numPathParams {
				return fmt.Errorf("accepts %d arg(s), received %d", numPathParams, len(args))
			}
			return nil
		}

		if len(args) != expected {
			return fmt.Errorf("accepts %d arg(s), received %d", expected, len(args))
		}
		return nil
	}
}

// assembleBody builds a JSON body from shorthand positional args and promoted flags.
func assembleBody(sh *BodyShorthand, args []string, pathParamCount int, cmd *cobra.Command, bodySchema *base.SchemaProxy) ([]byte, error) {
	body := map[string]interface{}{}

	shorthandArgs := args[pathParamCount:]
	for i, mapping := range sh.Args {
		if i >= len(shorthandArgs) {
			break
		}
		val := shorthandArgs[i]
		switch mapping.Transform {
		case "string", "uuid":
			body[mapping.FieldPath] = val
		case "email-list":
			parts := strings.Split(val, ",")
			users := make([]map[string]string, 0, len(parts))
			for _, e := range parts {
				e = strings.TrimSpace(e)
				if e != "" {
					users = append(users, map[string]string{"email": e})
				}
			}
			body[mapping.FieldPath] = users
		default:
			body[mapping.FieldPath] = val
		}
	}

	for _, fm := range sh.Flags {
		flag := cmd.Flags().Lookup(fm.FlagName)
		if flag == nil {
			continue
		}
		val := flag.Value.String()
		if val == "" {
			continue
		}
		fieldType, err := shorthandFieldType(bodySchema, fm.FieldPath)
		if err != nil {
			return nil, fmt.Errorf("invalid shorthand --%s: %w", fm.FlagName, err)
		}
		if fieldType == "boolean" {
			parsed, err := strconv.ParseBool(val)
			if err != nil {
				return nil, fmt.Errorf("invalid --%s value %q: expected a boolean", fm.FlagName, val)
			}
			body[fm.FieldPath] = parsed
		} else {
			body[fm.FieldPath] = val
		}
	}

	return json.Marshal(body)
}

// shorthandFieldType resolves a promoted flag against the operation's request
// body schema. The shorthand registry chooses which fields get a convenient
// CLI flag; the OpenAPI document remains authoritative for their JSON types.
func shorthandFieldType(bodySchema *base.SchemaProxy, fieldPath string) (string, error) {
	if bodySchema == nil {
		return "string", nil
	}
	fieldSchema, err := resolveField(bodySchema, fieldPath, "field")
	if err != nil {
		return "", fmt.Errorf("field %q is not present in the request schema: %w", fieldPath, err)
	}
	return schemaType(fieldSchema), nil
}
