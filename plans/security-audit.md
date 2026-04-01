# Security Audit Report: omni-cli

**Date:** 2026-03-31
**Auditor:** Claude Code (Opus 4.6)
**Scope:** Full codebase and git history review for open-source readiness
**Branch:** nate/for-the-claudes (commit 388dad1)

---

## Executive Summary

A comprehensive security audit was performed covering secrets in git history, code-level vulnerabilities, and open-source hygiene. **No secrets or credentials were found in the codebase or git history.** Two blocking issues were identified: a missing LICENSE file and no HTTPS enforcement on the API base URL. Several medium and low severity issues were also found.

---

## Blocking Issues

### 1. Missing LICENSE File

- **Severity:** Critical
- **Location:** Repository root
- **Description:** No `LICENSE` or `COPYING` file exists. Without a license, the code is legally "all rights reserved" and cannot be used, modified, or distributed by anyone.
- **Recommendation:** Add a LICENSE file (MIT, Apache 2.0, etc.) before publishing.

### 2. No HTTPS Enforcement on Base URL

- **Severity:** High
- **Location:** `internal/auth/auth.go:16`, `internal/config/config.go:37-88`
- **Description:** The `auth.Do()` function constructs request URLs by concatenating `cfg.BaseURL + path` with no validation that the base URL uses HTTPS. The `config.Resolve()` function validates that `BaseURL` is non-empty but does not check the scheme. A user could configure `http://` as the base URL (via config file, env var, or `--base-url` flag), causing the API Bearer token to be transmitted in plaintext. An attacker on the same network could intercept the token.
- **Recommendation:** Validate that `cfg.BaseURL` starts with `https://` in `config.Resolve()`, or emit a warning to stderr when HTTP is used. Allow an explicit opt-out flag (e.g., `--insecure`) for local development.

---

## Medium Severity Issues

### 3. API Token Stored as Plaintext on Disk

- **Severity:** Medium
- **Location:** `internal/config/config.go:107-117`
- **Description:** The `Save()` function writes the config file containing `apiKey` in plaintext JSON. File permissions are correctly set to `0o600` (directory `0o700`), restricting access to the owning user. However, plaintext storage means any process running as the same user, or any backup system, can read the API key.
- **Recommendation:** Consider integrating with the OS keychain (macOS Keychain, Windows Credential Manager, Linux secret-service) for token storage. Current file permissions are a reasonable baseline.

### 4. API Token Visible in Process List via --token Flag

- **Severity:** Medium
- **Location:** `cmd/omni/main.go:42`
- **Description:** The `--token` flag accepts the API token as a command-line argument. On Unix systems, command-line arguments are visible to all users via `ps aux` or `/proc/*/cmdline`. Running `omni models list --token secret-api-key` exposes the token to any user on the system.
- **Recommendation:** Document that `--token` is less secure than environment variables. Consider reading the token from a file descriptor or stdin when passed interactively. Add a warning in the `--token` flag help text.

### 5. SSRF-like Token Forwarding via Base URL

- **Severity:** Medium
- **Location:** `internal/auth/auth.go:15-16`, `internal/config/config.go:37-88`
- **Description:** The base URL is fully user-controlled (via config, env, or flag) and used directly to construct HTTP requests with no validation. A malicious config file could set the base URL to an internal network address (e.g., `http://169.254.169.254/` for cloud metadata endpoints), causing the CLI to send the Bearer token to an arbitrary server. This matters in scenarios where config files are shared or set via environment variables in CI/CD.
- **Recommendation:** Validate that the base URL matches expected Omni API domains, or warn when the base URL does not match `*.omni.co`.

---

## Low Severity Issues

### 6. Redaction Panic on Short API Keys

- **Severity:** Low
- **Location:** `cmd/omni/config_commands.go:99-101`
- **Description:** The redaction logic `p.APIKey[:4] + "..." + p.APIKey[len(p.APIKey)-4:]` will panic with an index-out-of-range error if the API key is fewer than 8 characters. For keys of 4-7 characters, it would also reveal the entire key since the first and last 4 characters overlap.
- **Recommendation:** Add a length check: if the key is shorter than 12 characters, redact the entire key (e.g., show `****`).

### 7. Unbounded stdin Read

- **Severity:** Low
- **Location:** `internal/openapi/generate.go:332-334`
- **Description:** The `readStdin()` function calls `io.ReadAll(os.Stdin)` with no size limit. Piping a very large file (e.g., `cat /dev/zero | omni ...`) will consume unbounded memory until the process is OOM-killed.
- **Recommendation:** Use `io.LimitReader(os.Stdin, maxSize)` with a reasonable limit (e.g., 10 MB) and return a clear error when exceeded.

### 8. API Key Echoed to Terminal During config init

- **Severity:** Low
- **Location:** `cmd/omni/config_commands.go:53-55`
- **Description:** During `omni config init`, the API key is read via `reader.ReadString('\n')` which echoes typed characters to the terminal. Anyone observing the screen or a terminal recording can see the full API key.
- **Recommendation:** Use `golang.org/x/term` `ReadPassword()` to suppress echo, similar to how `ssh` and `sudo` handle password input.

### 9. Hardcoded Internal Monorepo Path

- **Severity:** Low
- **Location:** `Makefile:21`
- **Description:** The `sync-spec` target contains `~/src/omni/omni/packages/bi-app/app/types/api/openapi/openapi.json`, revealing internal monorepo structure and not usable by external contributors.
- **Recommendation:** Make the path configurable via an environment variable or document it as internal-only.

---

## Positive Findings

| Area | Status | Details |
|------|--------|---------|
| Secrets in git history | Clean | Exhaustive search for passwords, API keys, private keys, tokens across all commits found only test fixtures with fake values (`"file-token"`, `"env-token"`) |
| Command injection | Not vulnerable | No `os/exec` usage; all user input flows through `net/http`/`net/url` safely |
| TLS defaults | Secure | Uses `http.DefaultClient` with Go's default cert validation; no `InsecureSkipVerify` |
| URL parameter escaping | Correct | Uses `url.PathEscape()` for path params, `url.Values` for query params |
| Embedded OpenAPI spec | Clean | No internal hostnames, staging URLs, or sensitive data; no `servers` block |
| Dependencies | All public | No private Go modules; `spf13/cobra` and `pb33f/libopenapi` are well-maintained |
| .env files | Never committed | Confirmed via full git history search |
| Git history | Clean | No internal ticket references, no sensitive content, no scrubbing needed |
| CI workflows | Clean | No secrets or internal registry references in GitHub Actions |
| Copyright/proprietary notices | None | No conflicting headers in source files |

---

## .gitignore Improvements

Current `.gitignore` covers basics. Consider adding before open-sourcing:

```
*.pem
*.key
*.p12
*.pfx
.env*
config.json
```

---

## Pre-Open-Source Checklist

- [ ] Add LICENSE file
- [ ] Enforce HTTPS on base URL (or warn on HTTP)
- [ ] Guard redaction logic against short API keys
- [ ] Suppress API key echo during `config init`
- [ ] Clean up or parameterize `sync-spec` Makefile path
- [ ] Confirm `github.com/exploreomni` is the intended public GitHub org
- [ ] Harden `.gitignore` with additional patterns
- [ ] Bound stdin reads with `io.LimitReader`
