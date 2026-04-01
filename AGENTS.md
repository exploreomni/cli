# Omni CLI — For AI Agents

Run `omni agent-help` to get a concise guide to all commands, workflows, and flags.

## Quick Start
1. Set `OMNI_API_TOKEN` env var (or run `omni config init`)
2. Run `omni agent-help` to see available commands and common workflows
3. To answer data questions: `omni ai generate-query --body '{"modelId":"MODEL_ID","prompt":"your question","executeQuery":true}'`
4. To find model IDs: `omni models list --compact`
