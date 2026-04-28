Sync the OpenAPI spec into this CLI, surface what changed, validate the build, and open a PR.

## User context

The user may provide context like a spec path, an issue number to fix, or a single endpoint they care about:

> $ARGUMENTS

Parse `$ARGUMENTS` for any of:
- A path ending in `openapi.json` → use as the source spec.
- `#<n>` or `issues/<n>` or a full issue URL → record as the issue to reference in the commit/PR. Run `gh issue view <n> --repo exploreomni/cli` to read the body so you can speak to the specific endpoint or param being requested.
- Free text → treat as guidance for what the user expects to land (e.g. "add fullyResolved param").

If blank, infer the story from the diff.

## Resolve the source spec

The source of truth lives in the `exploreomni/omni` monorepo. Resolve the source path in this order (highest wins) and stop at the first that exists:

1. Path passed in `$ARGUMENTS`.
2. `$OMNI_OPENAPI_SPEC` env var.

If neither resolves, stop and ask the user for the path to their monorepo's `openapi.json`. Do NOT fetch from the network and do NOT guess a path.

## Steps

1. **Pre-flight** — Confirm `git status` is clean. If there are uncommitted changes, stop and show them; the sync touches `api/openapi.json` and `cmd/omni/openapi.json`, and we don't want to mix unrelated edits in.

2. **Preview the diff** — Without writing anything, run:
   ```
   diff -u api/openapi.json "$SOURCE_SPEC" | head -400
   ```
   Then summarize for the user, focusing on the parts they'll care about:
   - **New operations**: `grep -E '^\+.*"operationId":' api/openapi.json` after the copy, vs. before. (Or compute it from the diff: `git diff api/openapi.json | grep -E '^\+.*"operationId":'`.)
   - **New params**: lines matching `^\+.*"name":` inside the diff that weren't present before.
   - **Removed operations / params** (rare but important): `^-.*"operationId":` and `^-.*"name":`.

   If the user gave a specific endpoint or param in `$ARGUMENTS`, confirm it actually appears in the new diff. If it doesn't, stop and tell the user — the upstream change probably hasn't been merged into their monorepo checkout yet.

3. **Branch** — Check the current branch first (`git branch --show-current`).
   - If you're already on a non-default branch — including a worktree branch (e.g. `claude/...`) or any existing topic branch — stay on it. Don't branch a branch.
   - Only when you're on the default branch (`main`), create a topic branch:
     ```
     git checkout -b sync-spec-<short-slug>
     ```
     The slug should describe the headline change (e.g. `sync-spec-fully-resolved`, `sync-spec-models-rename`). If `$ARGUMENTS` referenced an issue, derive the slug from the issue title.

4. **Sync** — Run the project's sync target. It copies into both `api/openapi.json` and `cmd/omni/openapi.json` (the latter is embedded into the binary):
   ```
   OMNI_OPENAPI_SPEC=<resolved path> make sync-spec
   ```

5. **Build & test** — These are the gate. If either fails, stop and show the errors:
   ```
   make build
   make test
   ```

6. **Validate the new surface area** — For each new operation and each new query/path param surfaced in step 2, run `--help` to confirm cobra wired it up:
   ```
   ./bin/omni <group> <command> --help
   ```
   New body fields on POST/PATCH endpoints won't appear as flags (those endpoints take `--body`); note that in the PR body rather than treating it as missing.

   If the user asked to verify against a live API and has a profile configured, run a read-only call (typically a GET) that exercises the new param. Never write/mutate without explicit confirmation.

7. **Commit** — One commit. Subject follows the project's convention (`sync spec: <headline>`), body lists the headline change first and any side-effect changes the full sync pulled in. Append the co-author trailer:
   ```
   Co-Authored-By: Claude <noreply@anthropic.com>
   ```
   If `$ARGUMENTS` referenced an issue, end the body with `Fixes #<n>`.

8. **Push & open the PR** — `git push -u origin HEAD`, then `gh pr create` with:
   - Title: same as the commit subject, suffixed with `(#<n>)` if there's a referenced issue.
   - Body: a `## Summary` section calling out the headline change and any side-effect operations/params, with a link to the upstream monorepo PR if the issue body included one. Followed by a `## Test plan` checklist (`make test`, `--help` output, optional live verification).

9. **Report** — Print the PR URL and a one-line summary of what landed.

## Notes

- This skill is read-only against the monorepo — it copies the spec out, never modifies it.
- Don't hand-edit the JSON. If the upstream spec is wrong, the fix belongs in the monorepo, not here.
- The full sync is intentional: a single PR per spec bump keeps the embedded JSON consistent. Surface side-effect changes in the PR body rather than trying to filter the diff.
