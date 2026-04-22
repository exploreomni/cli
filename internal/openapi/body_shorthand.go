package openapi

import (
	"encoding/json"
	"fmt"
	"strings"

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
	IsBool      bool
	Transform   string // "string" (default), "string-list", "scim-member-list", "json"
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
			{FlagName: "run-query", FieldPath: "runQuery", Description: "execute the generated query (server default: true)", IsBool: true},
			{FlagName: "user-id", FieldPath: "userId", Description: "user ID to execute as"},
			{FlagName: "workbook-url", FieldPath: "workbookUrl", Description: "workbook URL for context"},
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
			{FlagName: "progress-webhook-enabled", FieldPath: "progressWebhookEnabled", Description: "enable progress webhooks", IsBool: true},
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

	// Documents
	"documentsCreateDraft": {
		Flags: []FlagMapping{
			{FlagName: "branch-id", FieldPath: "branchId", Description: "branch ID for the draft"},
		},
		ExampleShort: `omni documents create-draft <identifier> --branch-id <uuid>`,
		ExampleJSON:  `omni documents create-draft <identifier> --body '{"branchId":"<uuid>"}'`,
	},
	"documentsDiscardDraft": {
		Flags: []FlagMapping{
			{FlagName: "branch-id", FieldPath: "branchId", Description: "branch ID for the draft"},
		},
		ExampleShort: `omni documents discard-draft <identifier> --branch-id <uuid>`,
		ExampleJSON:  `omni documents discard-draft <identifier> --body '{"branchId":"<uuid>"}'`,
	},

	// Dashboards
	"dashboardsDownload": {
		Args: []ArgMapping{
			{Name: "format", FieldPath: "format", Description: "output format: pdf, png, csv, xlsx, or json", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "filename", FieldPath: "filename", Description: "custom filename (without extension)"},
			{FlagName: "filter-config", FieldPath: "filterConfig", Description: "filter conditions as JSON object", Transform: "json"},
			{FlagName: "query-identifier-map-key", FieldPath: "queryIdentifierMapKey", Description: "query ID for single-tile tasks"},
			{FlagName: "paper-format", FieldPath: "paperFormat", Description: "pdf paper size: a3, a4, letter, legal, fit_page, tabloid"},
			{FlagName: "paper-orientation", FieldPath: "paperOrientation", Description: "pdf orientation: portrait or landscape"},
			{FlagName: "max-row-limit", FieldPath: "maxRowLimit", Description: "max rows (csv/json/xlsx)", Transform: "json"},
			{FlagName: "override-row-limit", FieldPath: "overrideRowLimit", Description: "override default row limit", IsBool: true},
			{FlagName: "enable-formatting", FieldPath: "enableFormatting", Description: "enable formatting in output", IsBool: true},
			{FlagName: "hide-hidden-fields", FieldPath: "hideHiddenFields", Description: "omit hidden fields from output", IsBool: true},
			{FlagName: "hide-title", FieldPath: "hideTitle", Description: "hide content title in output", IsBool: true},
			{FlagName: "show-content-link", FieldPath: "showContentLink", Description: "include link to content in output", IsBool: true},
			{FlagName: "show-filters", FieldPath: "showFilters", Description: "include filters in output", IsBool: true},
			{FlagName: "single-column-layout", FieldPath: "singleColumnLayout", Description: "stack tiles in a single column", IsBool: true},
			{FlagName: "expand-tables-to-show-all-rows", FieldPath: "expandTablesToShowAllRows", Description: "include up to 1000 rows in table visualizations", IsBool: true},
		},
		ExampleShort: `omni dashboards download <identifier> pdf --paper-format letter --paper-orientation landscape`,
		ExampleJSON:  `omni dashboards download <identifier> --body '{"format":"pdf","paperFormat":"letter","paperOrientation":"landscape"}'`,
	},

	// Connections
	"connectionsDbtUpdate": {
		Args: []ArgMapping{
			{Name: "branch", FieldPath: "branch", Description: "git branch name", Transform: "string"},
			{Name: "ssh-url", FieldPath: "sshUrl", Description: "SSH URL for git repository", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "autogen-relationships", FieldPath: "autogenRelationships", Description: "automatically generate relationships from dbt (required)", IsBool: true},
			{FlagName: "enable-virtual-schemas", FieldPath: "enableVirtualSchemas", Description: "enable virtual schemas from dbt (required)", IsBool: true},
			{FlagName: "dbt-version", FieldPath: "dbtVersion", Description: "dbt version (e.g. 1.10, 1.11, Auto)"},
			{FlagName: "enable-semantic-layer", FieldPath: "enableSemanticLayer", Description: "enable dbt semantic layer integration", IsBool: true},
			{FlagName: "project-root-path", FieldPath: "projectRootPath", Description: "path to dbt project root within repository"},
			{FlagName: "rotate-keys", FieldPath: "rotateKeys", Description: "rotate SSH deploy keys", IsBool: true},
		},
		ExampleShort: `omni connections dbt-update <connection-id> main git@github.com:org/repo.git --autogen-relationships true --enable-virtual-schemas false`,
		ExampleJSON:  `omni connections dbt-update <connection-id> --body '{"branch":"main","sshUrl":"git@github.com:org/repo.git","autogenRelationships":true,"enableVirtualSchemas":false}'`,
	},
	"connectionsDbtEnvironmentsCreate": {
		Args: []ArgMapping{
			{Name: "name", FieldPath: "name", Description: "environment name", Transform: "string"},
			{Name: "target-schema", FieldPath: "targetSchema", Description: "target schema for this environment", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "owner-id", FieldPath: "ownerId", Description: "user ID of the environment owner"},
			{FlagName: "target-database", FieldPath: "targetDatabase", Description: "target database override"},
			{FlagName: "target-name", FieldPath: "targetName", Description: "target name override"},
			{FlagName: "target-role", FieldPath: "targetRole", Description: "target role override"},
		},
		ExampleShort: `omni connections dbt-environments-create <connection-id> "PR_1111" "pr_1111"`,
		ExampleJSON:  `omni connections dbt-environments-create <connection-id> --body '{"name":"PR_1111","targetSchema":"pr_1111"}'`,
	},
	"connectionsDbtEnvironmentsUpdate": {
		Args: []ArgMapping{
			{Name: "name", FieldPath: "name", Description: "environment name", Transform: "string"},
			{Name: "target-schema", FieldPath: "targetSchema", Description: "target schema for this environment", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "owner-id", FieldPath: "ownerId", Description: "user ID of the environment owner"},
			{FlagName: "target-database", FieldPath: "targetDatabase", Description: "target database override"},
			{FlagName: "target-name", FieldPath: "targetName", Description: "target name override"},
			{FlagName: "target-role", FieldPath: "targetRole", Description: "target role override"},
		},
		ExampleShort: `omni connections dbt-environments-update <connection-id> <env-id> "PR_1111" "pr_1111"`,
		ExampleJSON:  `omni connections dbt-environments-update <connection-id> <env-id> --body '{"name":"PR_1111","targetSchema":"pr_1111"}'`,
	},
	"connectionsSchedulesCreate": {
		Args: []ArgMapping{
			{Name: "schedule", FieldPath: "schedule", Description: "AWS EventBridge cron expression", Transform: "string"},
			{Name: "timezone", FieldPath: "timezone", Description: "IANA timezone for schedule execution", Transform: "string"},
		},
		ExampleShort: `omni connections schedules-create <connection-id> "0 2 * * ? *" "America/New_York"`,
		ExampleJSON:  `omni connections schedules-create <connection-id> --body '{"schedule":"0 2 * * ? *","timezone":"America/New_York"}'`,
	},
	"connectionsSchedulesUpdate": {
		Args: []ArgMapping{
			{Name: "schedule", FieldPath: "schedule", Description: "AWS EventBridge cron expression", Transform: "string"},
			{Name: "timezone", FieldPath: "timezone", Description: "IANA timezone for schedule execution", Transform: "string"},
		},
		ExampleShort: `omni connections schedules-update <connection-id> <schedule-id> "0 2 * * ? *" "America/New_York"`,
		ExampleJSON:  `omni connections schedules-update <connection-id> <schedule-id> --body '{"schedule":"0 2 * * ? *","timezone":"America/New_York"}'`,
	},

	// Tier 4: Flags-only promotion (no new positional args)
	"documentsUpdate": {
		Flags: []FlagMapping{
			{FlagName: "name", FieldPath: "name", Description: "new document name"},
			{FlagName: "description", FieldPath: "description", Description: "document description"},
			{FlagName: "clear-existing-draft", FieldPath: "clearExistingDraft", Description: "clear existing draft before updating", IsBool: true},
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
}

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

	// Register promoted flags (all as strings; bool flags are parsed from
	// their string value in assembleBody)
	for _, f := range sh.Flags {
		cmd.Flags().String(f.FlagName, f.Default, f.Description)
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

		// If --body/--json-body is provided, use existing behavior
		if rawBody != "" {
			return originalRunE(cmd, args)
		}

		// Assemble body from shorthand args and promoted flags
		body, err := assembleBody(sh, args, numPathParams, cmd)
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
func assembleBody(sh *BodyShorthand, args []string, pathParamCount int, cmd *cobra.Command) ([]byte, error) {
	body := map[string]interface{}{}

	shorthandArgs := args[pathParamCount:]
	for i, mapping := range sh.Args {
		if i >= len(shorthandArgs) {
			break
		}
		v, err := transformValue(mapping.Transform, shorthandArgs[i])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", mapping.Name, err)
		}
		body[mapping.FieldPath] = v
	}

	for _, fm := range sh.Flags {
		val, _ := cmd.Flags().GetString(fm.FlagName)
		if val == "" {
			continue
		}
		if fm.IsBool {
			body[fm.FieldPath] = (val == "true")
			continue
		}
		v, err := transformValue(fm.Transform, val)
		if err != nil {
			return nil, fmt.Errorf("--%s: %w", fm.FlagName, err)
		}
		body[fm.FieldPath] = v
	}

	return json.Marshal(body)
}

// transformValue converts a CLI string value into the JSON shape indicated by the transform.
func transformValue(transform, val string) (interface{}, error) {
	switch transform {
	case "", "string", "uuid":
		return val, nil
	case "email-list":
		parts := strings.Split(val, ",")
		users := make([]map[string]string, 0, len(parts))
		for _, e := range parts {
			e = strings.TrimSpace(e)
			if e != "" {
				users = append(users, map[string]string{"email": e})
			}
		}
		return users, nil
	case "string-list":
		parts := strings.Split(val, ",")
		out := make([]string, 0, len(parts))
		for _, s := range parts {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	case "scim-member-list":
		parts := strings.Split(val, ",")
		members := make([]map[string]string, 0, len(parts))
		for _, id := range parts {
			id = strings.TrimSpace(id)
			if id != "" {
				members = append(members, map[string]string{"value": id})
			}
		}
		return members, nil
	case "json":
		var parsed interface{}
		if err := json.Unmarshal([]byte(val), &parsed); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		return parsed, nil
	default:
		return val, nil
	}
}
