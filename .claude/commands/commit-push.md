Ship staged and unstaged changes as a series of logical, conventional commits.

## User context

The user may provide additional context to guide how changes are grouped or described:

> $ARGUMENTS

If the user provided context above, use it to inform commit grouping, message wording, and ordering. If blank, infer the story from the diff.

## Steps

1. **Gate on quality** — Run the project's lint and test suites. If either fails, stop and show the errors. Do NOT commit anything.
   ```
   npm run lint
   npm test
   ```

2. **Inventory changes** — Run `git diff HEAD --stat` and `git diff HEAD` to understand every changed, added, and deleted file. Include untracked files via `git ls-files --others --exclude-standard`.

3. **Plan the commit sequence** — Group the changes into the smallest set of commits that tells a clear, linear story a human reviewer can follow. Each commit must:
   - Use a conventional-commit prefix (`feat:`, `fix:`, `refactor:`, `chore:`, `docs:`, `test:`, `style:`, `ci:`).
   - Be self-contained — the repo should build after every commit.
   - Be ordered so foundational changes (types, schemas, config) come before the code that depends on them, and tests/docs come last.
   - Have a concise subject line (<72 chars) and, when non-obvious, a body explaining *why*.

   Present the plan to the user as a numbered list showing which files go in each commit and the proposed message. **Wait for approval before continuing.**

4. **Execute the commits** — For each planned commit:
   - Stage only the relevant files (`git add <file> ...`).
   - Commit with the approved message. Append the co-author trailer:
     ```
     Co-Authored-By: Claude <noreply@anthropic.com>
     ```
   - Do NOT use `--no-verify`.

5. **Push** — Run `git push` (with `-u origin HEAD` if no upstream is set). If the push is rejected, tell the user rather than force-pushing.
