Cut a new release of the omni CLI by tagging `main` and pushing the tag.

A release here is just `git tag <version>` followed by `git push origin <version>`. There is no separate build/upload step — that's wired up downstream from the tag push.

## User context

The user may pass arguments controlling the version and whether to actually release:

> $ARGUMENTS

Parse `$ARGUMENTS` for two independent things:

**Version selector** (at most one):

- Empty → defaults to `patch` (bump the patch component of the latest tag by 1, e.g. `v1.0.2` → `v1.0.3`).
- `patch` → same as the default. Accept it explicitly so users can be unambiguous.
- `minor` → bump the minor component, reset patch to 0 (e.g. `v1.0.2` → `v1.1.0`).
- `major` → bump the major component, reset minor and patch to 0 (e.g. `v1.0.2` → `v2.0.0`).
- A literal version like `v2.2.0` (with or without leading `v` — normalize to include it) → use it verbatim.

**Dry-run flag** (optional, can be combined with any version selector):

- `--dry-run`, `-n`, or the bare word `dry-run` → run all the pre-flight checks and version computation, show what *would* happen, but **do not run `git tag` or `git push`** and do not prompt for confirmation. Examples: `/release-cli --dry-run`, `/release-cli minor --dry-run`, `/release-cli v2.2.0 --dry-run`.

Anything else: stop and ask the user what they meant.

## Steps

1. **Pre-flight** — All of these must pass; if any fails, stop and report it. Do not attempt to fix the working tree (no resets, no stashes) without explicit user direction.
   - `git rev-parse --abbrev-ref HEAD` is `main`.
   - `git status --porcelain` is empty (clean working tree).
   - `git fetch origin main` succeeds, then `git rev-list --count HEAD..origin/main` is `0` and `git rev-list --count origin/main..HEAD` is `0` (local main is exactly even with origin/main).

2. **Find the latest tag** — `git tag --sort=-v:refname | head -1`. Parse it as `vMAJOR.MINOR.PATCH`. **Print the current version to the user** (e.g. `Current version: v1.0.2`) so it's visible before they confirm. If there are no existing tags, treat the baseline as `v0.0.0` and tell the user no prior tag was found.

3. **Compute the target version** — Apply the rule from `$ARGUMENTS` to get the new version string. Always include the leading `v`.

4. **Sanity-check the jump** — Compare the target to the latest tag and craft a *semantic* confirmation message. Always include both the current and target version in the message so the user can eyeball the jump:
   - Normal patch/minor/major bump (one component up by 1, lower components zeroed where appropriate): `"Current version: <latest>. This will create release <version>. Confirm before proceeding:"`
   - Target is `<=` latest tag (would re-tag or move backwards): `"WARNING: <version> is not newer than the latest tag <latest>. This will fail or rewrite history. Are you sure?"`
   - Target tag already exists locally or on origin (`git rev-parse <version>` succeeds, or `git ls-remote --tags origin <version>` returns a row): `"WARNING: tag <version> already exists (current: <latest>). Refusing to overwrite — pick a different version."` and stop.
   - Major jump > 1 (e.g. `v1.x.x` → `v3.0.0`): `"WARNING: jumping from <latest> to <version> bumps the major version by N. That looks like a large jump and is probably a mistake. Are you sure you want to proceed?"`
   - Minor jump > 1 within the same major, or patch jump > 1 within the same minor: similar wording, calling out the actual delta and showing both versions.
   - Skipping components without zeroing the lower ones (e.g. `v1.0.2` → `v1.2.5`): call that out — `"Current version: <latest>. <version> skips the usual minor reset and is unusual, confirm?"`.
   - Otherwise (literal version that's a clean forward bump): `"Current version: <latest>. This will release version <version>. Confirm before proceeding:"`

   In **dry-run mode**, prefix the message with `[DRY RUN] ` and **do not** wait for a yes. Print the exact commands that *would* run (`git tag <version>` and `git push origin <version>`) and skip to the report step.

   In normal mode, show the message and **wait for an explicit yes** before continuing. Never tag without confirmation.

5. **Tag and push** — Skip this step entirely in dry-run mode. On confirmation:
   ```
   git tag <version>
   git push origin <version>
   ```
   If the push fails, surface the error verbatim and stop.

6. **Report** — In dry-run mode, print `[DRY RUN] No tag was created and nothing was pushed. Re-run without --dry-run to release <version>.` and stop. In normal mode, print the released version and the `git push` output, and mention that the tag is now on `origin` and any downstream release automation will pick it up from there.

## Notes

- This command never amends, force-pushes, or deletes tags. If something looks off (target already exists, tree dirty, branch behind), stop and tell the user — let them decide.
- Don't run `make build` / `make test` here. The gate for releases is that `main` is already green; this command just publishes a tag.
