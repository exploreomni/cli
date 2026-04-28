Sync the OpenAPI spec into this CLI, surface what changed, validate the build, and open a PR.

## User context

The user may provide context like a spec source, an issue number to fix, or a single endpoint they care about:

> $ARGUMENTS

Parse `$ARGUMENTS` for any of:
- A path ending in `openapi.json` → use as the source spec (sets `OMNI_OPENAPI_SPEC`).
- A bare ref name (e.g. `release-2026.04`) or `--ref <name>` → pin the upstream fetch to that branch/tag/commit (sets `OMNI_OPENAPI_REF`).
- `#<n>` or `issues/<n>` or a full issue URL → record as the issue to reference in the commit/PR. Run `gh issue view <n> --repo exploreomni/cli` to read the body so you can speak to the specific endpoint or param being requested.
- Free text → treat as guidance for what the user expects to land (e.g. "add fullyResolved param").

If blank, default to the gh-fetched `main` of `exploreomni/omni`. Don't ask for a source unless the default fails.

## How the spec is resolved

`make sync-spec` (defined in the Makefile) handles resolution:

1. If `OMNI_OPENAPI_SPEC` is set, it copies from that local path. Use this for testing unmerged spec changes against a local monorepo checkout.
2. Otherwise it fetches `exploreomni/omni`'s `openapi.json` via `gh api` (defaults to `main`; override with `OMNI_OPENAPI_REF`).

The skill's job is to drive that target, not re-implement resolution.

## Steps

1. **Pre-flight** — Confirm `git status` is clean. If there are uncommitted changes, stop and show them; the sync touches `api/openapi.json` and `cmd/omni/openapi.json`, and we don't want to mix unrelated edits in. If relying on the gh-fetch default, also confirm `gh auth status` is good.

2. **Branch** — Check the current branch first (`git branch --show-current`).
   - If you're already on a non-default branch — including a worktree branch (e.g. `claude/...`) or any existing topic branch — stay on it. Don't branch a branch.
   - Only when you're on the default branch (`main`), create a topic branch:
     ```
     git checkout -b sync-spec-<short-slug>
     ```
     The slug should describe the headline change (e.g. `sync-spec-fully-resolved`, `sync-spec-models-rename`). If `$ARGUMENTS` referenced an issue, derive the slug from the issue title.

3. **Sync** — Invoke the make target. Pass `OMNI_OPENAPI_SPEC` and/or `OMNI_OPENAPI_REF` only if the user supplied them in `$ARGUMENTS`:
   ```
   make sync-spec
   # or, with overrides:
   OMNI_OPENAPI_SPEC=/path/to/openapi.json make sync-spec
   OMNI_OPENAPI_REF=my-branch make sync-spec
   ```

4. **Inspect the diff** — Now that the spec is in place, look at what actually changed and summarize it for the user:
   ```
   git diff --stat api/openapi.json cmd/omni/openapi.json
   git diff api/openapi.json | head -400
   ```
   Pull out the parts the user cares about:
   - **New operations**: `git diff api/openapi.json | grep -E '^\+.*"operationId":'`
   - **New params**: `git diff api/openapi.json | grep -E '^\+.*"name":'` (filter out body schema field names — those aren't query/path params)
   - **Removed operations / params**: `^-.*"operationId":` and `^-.*"name":` (rare but important when present)

   If the user gave a specific endpoint or param in `$ARGUMENTS`, confirm it appears in the diff. If it doesn't, stop and tell the user — the upstream change probably hasn't been merged into the ref you fetched. Suggest passing `--ref <branch>` or `OMNI_OPENAPI_SPEC=<local path>` to point at a checkout that has the change.

   If the diff is empty, stop early and report "spec already up to date" — no commit, no PR.

5. **Build & test** — These are the gate. If either fails, stop and show the errors:
   ```
   make build
   make test
   ```

6. **Validate the new surface area** — For each new operation and each new query/path param surfaced in step 4, run `--help` to confirm cobra wired it up:
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

- The default sync hits the network via `gh` — without network or `gh` auth, set `OMNI_OPENAPI_SPEC` to a local file.
- Don't hand-edit the JSON. If the upstream spec is wrong, the fix belongs in the monorepo, not here.
- The full sync is intentional: a single PR per spec bump keeps the embedded JSON consistent. Surface side-effect changes in the PR body rather than trying to filter the diff.
