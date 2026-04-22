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

	// SCIM
	"scimUsersCreate": {
		Args: []ArgMapping{
			{Name: "user-name", FieldPath: "userName", Description: "email address (username) of the user", Transform: "string"},
			{Name: "display-name", FieldPath: "displayName", Description: "display name of the user", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "user-attributes", FieldPath: "urn:omni:params:1.0:UserAttribute", Description: "Omni user attributes as JSON object", Transform: "json"},
		},
		ExampleShort: `omni scim users-create user@example.com "John Doe"`,
		ExampleJSON:  `omni scim users-create --body '{"userName":"user@example.com","displayName":"John Doe"}'`,
	},
	"scimUsersReplace": {
		Args: []ArgMapping{
			{Name: "user-name", FieldPath: "userName", Description: "email address (username) of the user", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "display-name", FieldPath: "displayName", Description: "display name of the user"},
			{FlagName: "active", FieldPath: "active", Description: "whether the user is active", IsBool: true},
			{FlagName: "user-attributes", FieldPath: "urn:omni:params:1.0:UserAttribute", Description: "Omni user attributes as JSON object", Transform: "json"},
			{FlagName: "enterprise-attributes", FieldPath: "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User", Description: "enterprise SCIM user attributes as JSON object", Transform: "json"},
		},
		ExampleShort: `omni scim users-replace <id> user@example.com --display-name "John Doe" --active true`,
		ExampleJSON:  `omni scim users-replace <id> --body '{"userName":"user@example.com","displayName":"John Doe","active":true}'`,
	},

	// Models
	"modelsCacheReset": {
		Flags: []FlagMapping{
			{FlagName: "reset-at", FieldPath: "resetAt", Description: "ISO-8601 timestamp for when to reset the cache"},
		},
		ExampleShort: `omni models cache-reset <model-id> <policy-name> --reset-at 2024-01-15T12:00:00Z`,
		ExampleJSON:  `omni models cache-reset <model-id> <policy-name> --body '{"resetAt":"2024-01-15T12:00:00Z"}'`,
	},
	"modelsUpdateField": {
		Flags: []FlagMapping{
			{FlagName: "label", FieldPath: "label", Description: "field label"},
			{FlagName: "description", FieldPath: "description", Description: "field description"},
			{FlagName: "sql", FieldPath: "sql", Description: "SQL expression for the field"},
			{FlagName: "format", FieldPath: "format", Description: "field format"},
			{FlagName: "ai-context", FieldPath: "aiContext", Description: "AI context for the field"},
			{FlagName: "topic-context", FieldPath: "topicContext", Description: "topic context for the field"},
			{FlagName: "group-label", FieldPath: "groupLabel", Description: "group label"},
			{FlagName: "else-value", FieldPath: "elseValue", Description: "else value for grouped fields"},
			{FlagName: "new-field-name", FieldPath: "newFieldName", Description: "new field name (for rename)"},
			{FlagName: "new-view-name", FieldPath: "newViewName", Description: "new view name (for move)"},
			{FlagName: "hidden", FieldPath: "hidden", Description: "whether the field is hidden", IsBool: true},
			{FlagName: "ignored", FieldPath: "ignored", Description: "whether the field is ignored", IsBool: true},
			{FlagName: "is-calc", FieldPath: "isCalc", Description: "whether this is a calculation field", IsBool: true},
			{FlagName: "drill-fields", FieldPath: "drillFields", Description: "comma-separated drill-down fields", Transform: "string-list"},
			{FlagName: "tags", FieldPath: "tags", Description: "comma-separated tags", Transform: "string-list"},
			{FlagName: "synonyms", FieldPath: "synonyms", Description: "comma-separated synonyms", Transform: "string-list"},
			{FlagName: "sample-values", FieldPath: "sampleValues", Description: "comma-separated sample values", Transform: "string-list"},
			{FlagName: "group-names", FieldPath: "groupNames", Description: "comma-separated group names", Transform: "string-list"},
			{FlagName: "bin-labels", FieldPath: "binLabels", Description: "comma-separated bin labels", Transform: "string-list"},
			{FlagName: "bin-boundaries", FieldPath: "binBoundaries", Description: "JSON array of numeric bin boundaries", Transform: "json"},
			{FlagName: "filters", FieldPath: "filters", Description: "filters as JSON object", Transform: "json"},
			{FlagName: "group-filters", FieldPath: "groupFilters", Description: "group filters as JSON array", Transform: "json"},
		},
		ExampleShort: `omni models update-field <model-id> <view-name> <field-name> --label "Revenue" --hidden false --tags kpi,featured`,
		ExampleJSON:  `omni models update-field <model-id> <view-name> <field-name> --body '{"label":"Revenue","hidden":false,"tags":["kpi","featured"]}'`,
	},
	"modelsCreate": {
		Args: []ArgMapping{
			{Name: "connection-id", FieldPath: "connectionId", Description: "connection ID for the model", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "model-name", FieldPath: "modelName", Description: "name for the model"},
			{FlagName: "base-model-id", FieldPath: "baseModelId", Description: "base model ID for extension or branch models"},
			{FlagName: "model-kind", FieldPath: "modelKind", Description: "model kind: SCHEMA, SHARED, SHARED_EXTENSION, BRANCH"},
			{FlagName: "allow-as-workbook-base", FieldPath: "allowAsWorkbookBase", Description: "allow this model as a workbook base", IsBool: true},
			{FlagName: "uses-isolated-branches", FieldPath: "usesIsolatedBranches", Description: "show branches on extension page (SHARED_EXTENSION only)", IsBool: true},
			{FlagName: "access-grants", FieldPath: "accessGrants", Description: "access grants as JSON array", Transform: "json"},
		},
		ExampleShort: `omni models create <connection-id> --model-name "Sales" --model-kind SHARED`,
		ExampleJSON:  `omni models create --body '{"connectionId":"<connection-id>","modelName":"Sales","modelKind":"SHARED"}'`,
	},
	"modelsContentValidatorReplace": {
		Args: []ArgMapping{
			{Name: "find", FieldPath: "find", Description: "string to find", Transform: "string"},
			{Name: "replacement", FieldPath: "replacement", Description: "replacement string", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "find-or-replace-type", FieldPath: "find_or_replace_type", Description: "type: FIELD, VIEW, or TOPIC (required)"},
			{FlagName: "branch-id", FieldPath: "branch_id", Description: "optional branch ID"},
			{FlagName: "only-in-workbook-id", FieldPath: "only_in_workbook_id", Description: "limit replace scope to a specific workbook"},
			{FlagName: "include-personal-folders", FieldPath: "include_personal_folders", Description: "include personal folders in the scope", IsBool: true},
		},
		ExampleShort: `omni models content-validator-replace <model-id> old_name new_name --find-or-replace-type FIELD`,
		ExampleJSON:  `omni models content-validator-replace <model-id> --body '{"find":"old_name","replacement":"new_name","find_or_replace_type":"FIELD"}'`,
	},
	"modelsYamlCreate": {
		Args: []ArgMapping{
			{Name: "file-name", FieldPath: "fileName", Description: "file name to create or update", Transform: "string"},
			{Name: "yaml", FieldPath: "yaml", Description: "YAML content (use \"-\" to read from stdin via --body)", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "branch-id", FieldPath: "branchId", Description: "branch ID for branch-aware operations"},
			{FlagName: "mode", FieldPath: "mode", Description: "IDE mode: combined, extension, staged, merged, history"},
			{FlagName: "commit-message", FieldPath: "commitMessage", Description: "commit message for git sync"},
			{FlagName: "fetched-at-millis", FieldPath: "fetchedAtMillis", Description: "timestamp when the file was fetched", Transform: "json"},
			{FlagName: "previous-checksum", FieldPath: "previousChecksum", Description: "previous checksum for conflict detection"},
		},
		ExampleShort: `omni models yaml-create <model-id> my_view.view.yaml "view:\n  name: my_view\n"`,
		ExampleJSON:  `omni models yaml-create <model-id> --body '{"fileName":"my_view.view.yaml","yaml":"view:\n  name: my_view\n"}'`,
	},
	"modelsCreateField": {
		Args: []ArgMapping{
			{Name: "view-name", FieldPath: "viewName", Description: "view to add the field to", Transform: "string"},
			{Name: "field-name", FieldPath: "fieldName", Description: "new field name", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "label", FieldPath: "label", Description: "field label"},
			{FlagName: "description", FieldPath: "description", Description: "field description"},
			{FlagName: "sql", FieldPath: "sql", Description: "SQL expression for the field"},
			{FlagName: "format", FieldPath: "format", Description: "field format"},
			{FlagName: "aggregate-type", FieldPath: "aggregateType", Description: "aggregate type for measures"},
			{FlagName: "ai-context", FieldPath: "aiContext", Description: "AI context for the field"},
			{FlagName: "topic-context", FieldPath: "topicContext", Description: "topic context for topic-scoped fields"},
			{FlagName: "tags", FieldPath: "tags", Description: "comma-separated tags", Transform: "string-list"},
			{FlagName: "hidden", FieldPath: "hidden", Description: "whether the field is hidden", IsBool: true},
		},
		ExampleShort: `omni models create-field <model-id> orders total_revenue --sql "SUM(amount)" --aggregate-type sum`,
		ExampleJSON:  `omni models create-field <model-id> --body '{"viewName":"orders","fieldName":"total_revenue","sql":"SUM(amount)"}'`,
	},
	"modelsGitCreate": {
		Args: []ArgMapping{
			{Name: "ssh-url", FieldPath: "sshUrl", Description: "SSH URL of the git repository", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "web-url", FieldPath: "webUrl", Description: "custom web URL for the git repository"},
			{FlagName: "base-branch", FieldPath: "baseBranch", Description: "target branch for Omni pull requests (default: main)"},
			{FlagName: "git-service-provider", FieldPath: "gitServiceProvider", Description: "git provider: github, gitlab, azure_devops, bitbucket, bitbucket_datacenter, auto"},
			{FlagName: "model-path", FieldPath: "modelPath", Description: "path to model files in the repository"},
			{FlagName: "require-pull-request", FieldPath: "requirePullRequest", Description: "when PRs are required: always, users-only, never"},
			{FlagName: "branch-per-pull-request", FieldPath: "branchPerPullRequest", Description: "create a branch in Omni for every PR", IsBool: true},
			{FlagName: "git-follower", FieldPath: "gitFollower", Description: "make the shared model read-only", IsBool: true},
		},
		ExampleShort: `omni models git-create <model-id> git@github.com:org/repo.git --base-branch main`,
		ExampleJSON:  `omni models git-create <model-id> --body '{"sshUrl":"git@github.com:org/repo.git","baseBranch":"main"}'`,
	},
	"modelsGitUpdate": {
		Flags: []FlagMapping{
			{FlagName: "ssh-url", FieldPath: "sshUrl", Description: "SSH URL of the git repository"},
			{FlagName: "web-url", FieldPath: "webUrl", Description: "custom web URL for the git repository"},
			{FlagName: "base-branch", FieldPath: "baseBranch", Description: "target branch for Omni pull requests"},
			{FlagName: "git-service-provider", FieldPath: "gitServiceProvider", Description: "git provider: github, gitlab, azure_devops, bitbucket, bitbucket_datacenter, auto"},
			{FlagName: "model-path", FieldPath: "modelPath", Description: "path to model files in the repository"},
			{FlagName: "require-pull-request", FieldPath: "requirePullRequest", Description: "when PRs are required: always, users-only, never"},
			{FlagName: "branch-per-pull-request", FieldPath: "branchPerPullRequest", Description: "create a branch in Omni for every PR", IsBool: true},
			{FlagName: "git-follower", FieldPath: "gitFollower", Description: "make the shared model read-only", IsBool: true},
		},
		ExampleShort: `omni models git-update <model-id> --base-branch develop --require-pull-request always`,
		ExampleJSON:  `omni models git-update <model-id> --body '{"baseBranch":"develop","requirePullRequest":"always"}'`,
	},
	"modelsUpdateView": {
		Flags: []FlagMapping{
			{FlagName: "new-view-name", FieldPath: "newViewName", Description: "new view name (for rename)"},
			{FlagName: "label", FieldPath: "label", Description: "view label"},
			{FlagName: "description", FieldPath: "description", Description: "view description"},
			{FlagName: "format", FieldPath: "format", Description: "view format"},
			{FlagName: "ai-context", FieldPath: "aiContext", Description: "AI context for the view"},
			{FlagName: "hidden", FieldPath: "hidden", Description: "whether the view is hidden", IsBool: true},
			{FlagName: "tags", FieldPath: "tags", Description: "comma-separated tags", Transform: "string-list"},
		},
		ExampleShort: `omni models update-view <model-id> <view-name> --label "Orders" --tags revenue,monthly`,
		ExampleJSON:  `omni models update-view <model-id> <view-name> --body '{"label":"Orders","tags":["revenue","monthly"]}'`,
	},
	"modelsUpdateTopic": {
		Flags: []FlagMapping{
			{FlagName: "new-topic-name", FieldPath: "newTopicName", Description: "new topic name (for rename)"},
			{FlagName: "label", FieldPath: "label", Description: "topic label"},
			{FlagName: "description", FieldPath: "description", Description: "topic description"},
			{FlagName: "group-label", FieldPath: "groupLabel", Description: "group label for the topic"},
			{FlagName: "hidden", FieldPath: "hidden", Description: "whether the topic is hidden", IsBool: true},
		},
		ExampleShort: `omni models update-topic <model-id> <topic-name> --label "Revenue" --hidden false`,
		ExampleJSON:  `omni models update-topic <model-id> <topic-name> --body '{"label":"Revenue","hidden":false}'`,
	},
	"modelsMergeBranch": {
		Flags: []FlagMapping{
			{FlagName: "commit-message", FieldPath: "commit_message", Description: "custom commit message for git sync"},
			{FlagName: "delete-branch", FieldPath: "delete_branch", Description: "delete the branch after merging", IsBool: true},
			{FlagName: "force-override-git-settings", FieldPath: "force_override_git_settings", Description: "override PR-required or git-follower settings", IsBool: true},
			{FlagName: "publish-drafts", FieldPath: "publish_drafts", Description: "publish branch-attached drafts", IsBool: true},
		},
		ExampleShort: `omni models merge-branch <model-id> <branch-name> --commit-message "Merging feature" --delete-branch true`,
		ExampleJSON:  `omni models merge-branch <model-id> <branch-name> --body '{"commit_message":"Merging feature","delete_branch":true}'`,
	},

	// Labels
	"labelsUpdate": {
		Flags: []FlagMapping{
			{FlagName: "name", FieldPath: "name", Description: "label name"},
			{FlagName: "color", FieldPath: "color", Description: "hex color (e.g. #0366d6)"},
			{FlagName: "description", FieldPath: "description", Description: "label description"},
			{FlagName: "homepage", FieldPath: "homepage", Description: "show label on homepage (admin only)", IsBool: true},
			{FlagName: "verified", FieldPath: "verified", Description: "mark as verified label (admin only)", IsBool: true},
		},
		ExampleShort: `omni labels update <name> --color "#0366d6" --description "updated"`,
		ExampleJSON:  `omni labels update <name> --body '{"color":"#0366d6","description":"updated"}'`,
	},

	// Folders
	"foldersAddPermissions": {
		Args: []ArgMapping{
			{Name: "role", FieldPath: "role", Description: "role to grant: NO_ACCESS, VIEWER, EXPLORER, EDITOR, MANAGER", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "user-ids", FieldPath: "userIds", Description: "comma-separated user UUIDs", Transform: "string-list"},
			{FlagName: "user-group-ids", FieldPath: "userGroupIds", Description: "comma-separated user group IDs", Transform: "string-list"},
			{FlagName: "access-boost", FieldPath: "accessBoost", Description: "grant access boost", IsBool: true},
		},
		ExampleShort: `omni folders add-permissions <folder-id> VIEWER --user-ids uuid1,uuid2`,
		ExampleJSON:  `omni folders add-permissions <folder-id> --body '{"role":"VIEWER","userIds":["uuid1","uuid2"]}'`,
	},
	"foldersUpdate": {
		Flags: []FlagMapping{
			{FlagName: "name", FieldPath: "name", Description: "new display name"},
			{FlagName: "path", FieldPath: "path", Description: "new URL path segment (alphanumeric and dashes)"},
			{FlagName: "resolve-path-conflict", FieldPath: "resolvePathConflict", Description: "auto-resolve path collisions with a numeric suffix", IsBool: true},
		},
		ExampleShort: `omni folders update <folder-id> --name "Q1 Reports" --path q1-reports`,
		ExampleJSON:  `omni folders update <folder-id> --body '{"name":"Q1 Reports","path":"q1-reports"}'`,
	},
	"foldersUpdatePermissions": {
		Flags: []FlagMapping{
			{FlagName: "role", FieldPath: "role", Description: "new role: NO_ACCESS, VIEWER, EXPLORER, EDITOR, MANAGER"},
			{FlagName: "user-ids", FieldPath: "userIds", Description: "comma-separated user UUIDs", Transform: "string-list"},
			{FlagName: "user-group-ids", FieldPath: "userGroupIds", Description: "comma-separated user group IDs", Transform: "string-list"},
			{FlagName: "access-boost", FieldPath: "accessBoost", Description: "access boost setting", IsBool: true},
		},
		ExampleShort: `omni folders update-permissions <folder-id> --role EDITOR --user-ids uuid1,uuid2`,
		ExampleJSON:  `omni folders update-permissions <folder-id> --body '{"role":"EDITOR","userIds":["uuid1","uuid2"]}'`,
	},
	"foldersRevokePermissions": {
		Flags: []FlagMapping{
			{FlagName: "user-ids", FieldPath: "userIds", Description: "comma-separated user UUIDs to revoke", Transform: "string-list"},
			{FlagName: "user-group-ids", FieldPath: "userGroupIds", Description: "comma-separated user group IDs to revoke", Transform: "string-list"},
		},
		ExampleShort: `omni folders revoke-permissions <folder-id> --user-ids uuid1,uuid2`,
		ExampleJSON:  `omni folders revoke-permissions <folder-id> --body '{"userIds":["uuid1","uuid2"]}'`,
	},

	// Embed
	"embedSsoGenerateSession": {
		Args: []ArgMapping{
			{Name: "external-id", FieldPath: "externalId", Description: "external identifier for the user", Transform: "string"},
			{Name: "name", FieldPath: "name", Description: "display name for the user", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "groups", FieldPath: "groups", Description: "comma-separated group names", Transform: "string-list"},
			{FlagName: "user-attributes", FieldPath: "userAttributes", Description: "user attributes as JSON object", Transform: "json"},
		},
		ExampleShort: `omni embed sso-generate-session user-123 "John Doe" --groups engineering,sales`,
		ExampleJSON:  `omni embed sso-generate-session --body '{"externalId":"user-123","name":"John Doe","groups":["engineering","sales"]}'`,
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
	"documentsAddPermits": {
		Args: []ArgMapping{
			{Name: "role", FieldPath: "role", Description: "role to grant: NO_ACCESS, VIEWER, EXPLORER, EDITOR, MANAGER", Transform: "string"},
		},
		Flags: []FlagMapping{
			{FlagName: "user-ids", FieldPath: "userIds", Description: "comma-separated user membership UUIDs", Transform: "string-list"},
			{FlagName: "user-group-ids", FieldPath: "userGroupIds", Description: "comma-separated user group IDs", Transform: "string-list"},
			{FlagName: "access-boost", FieldPath: "accessBoost", Description: "grant access boost", IsBool: true},
		},
		ExampleShort: `omni documents add-permits <identifier> VIEWER --user-ids uuid1,uuid2`,
		ExampleJSON:  `omni documents add-permits <identifier> --body '{"role":"VIEWER","userIds":["uuid1","uuid2"]}'`,
	},
	"documentsUpdatePermits": {
		Flags: []FlagMapping{
			{FlagName: "role", FieldPath: "role", Description: "new role: NO_ACCESS, VIEWER, EXPLORER, EDITOR, MANAGER"},
			{FlagName: "user-ids", FieldPath: "userIds", Description: "comma-separated user membership UUIDs", Transform: "string-list"},
			{FlagName: "user-group-ids", FieldPath: "userGroupIds", Description: "comma-separated user group IDs", Transform: "string-list"},
			{FlagName: "access-boost", FieldPath: "accessBoost", Description: "access boost setting", IsBool: true},
		},
		ExampleShort: `omni documents update-permits <identifier> --role EDITOR --user-ids uuid1,uuid2`,
		ExampleJSON:  `omni documents update-permits <identifier> --body '{"role":"EDITOR","userIds":["uuid1","uuid2"]}'`,
	},
	"documentsRevokePermits": {
		Flags: []FlagMapping{
			{FlagName: "user-ids", FieldPath: "userIds", Description: "comma-separated user membership UUIDs to revoke", Transform: "string-list"},
			{FlagName: "user-group-ids", FieldPath: "userGroupIds", Description: "comma-separated user group IDs to revoke", Transform: "string-list"},
		},
		ExampleShort: `omni documents revoke-permits <identifier> --user-ids uuid1,uuid2`,
		ExampleJSON:  `omni documents revoke-permits <identifier> --body '{"userIds":["uuid1","uuid2"]}'`,
	},
	"documentsBulkUpdateLabels": {
		Flags: []FlagMapping{
			{FlagName: "add", FieldPath: "add", Description: "comma-separated labels to add", Transform: "string-list"},
			{FlagName: "remove", FieldPath: "remove", Description: "comma-separated labels to remove", Transform: "string-list"},
		},
		ExampleShort: `omni documents bulk-update-labels <identifier> --add alpha,beta --remove gamma`,
		ExampleJSON:  `omni documents bulk-update-labels <identifier> --body '{"add":["alpha","beta"],"remove":["gamma"]}'`,
	},
	"documentsUpdatePermissionSettings": {
		Flags: []FlagMapping{
			{FlagName: "organization-role", FieldPath: "organizationRole", Description: "org-wide role: viewer, editor, manager, no_access"},
			{FlagName: "organization-access-boost", FieldPath: "organizationAccessBoost", Description: "boost organization access", IsBool: true},
			{FlagName: "can-download", FieldPath: "canDownload", Description: "allow downloading", IsBool: true},
			{FlagName: "can-drill", FieldPath: "canDrill", Description: "allow drill-down", IsBool: true},
			{FlagName: "can-schedule", FieldPath: "canSchedule", Description: "allow scheduling", IsBool: true},
			{FlagName: "can-upload", FieldPath: "canUpload", Description: "allow uploads", IsBool: true},
			{FlagName: "can-use-dashboard-ai", FieldPath: "canUseDashboardAi", Description: "allow using dashboard AI", IsBool: true},
			{FlagName: "can-view-workbook", FieldPath: "canViewWorkbook", Description: "allow viewing workbook", IsBool: true},
			{FlagName: "require-pull-request-to-publish", FieldPath: "requirePullRequestToPublish", Description: "require PR to publish changes", IsBool: true},
		},
		ExampleShort: `omni documents update-permission-settings <identifier> --organization-role viewer --can-download false`,
		ExampleJSON:  `omni documents update-permission-settings <identifier> --body '{"organizationRole":"viewer","canDownload":false}'`,
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
