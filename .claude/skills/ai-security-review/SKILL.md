---
name: ai-security-review
description: Security review for the GPSR Compliance Engine and its harness — prompt injection (especially storefront/merchant input the app processes), secret leakage, OWASP LLM Top 10, and auditing .claude/settings.json deny rules and PreToolUse hooks. Use for any security review, threat model, guardrail, "is this safe", secrets-handling, or settings/hook task. Triggers on "security review", "threat model", "prompt injection", "check for secrets", "harden", "review settings.json", and re-work like "did we fix the finding".
---

# AI Security Review (GPSR)

Two surfaces to defend: the **product** (end-user/storefront input that reaches the
backend or any LLM call) and the **harness** (making the deterministic guardrails
actually enforce the advisory rules in AGENTS.md).

## First Principle
**Prompt is probabilistic; guardrail is deterministic.** AGENTS.md rules have no
enforcement — they only raise the probability of good behavior, and context drifts.
Push every security-critical constraint into `.claude/settings.json` permissions and
hooks. Invest at the **action layer** (blocking permissions) over the language layer
(asking nicely). A guardrail assumes the AI will be fooled, then blocks the consequence.

## Defense in Depth (6 layers)
Input → Instruction → Permission → Sandbox → Output → Human. Don't rely on one. The
highest-leverage layer for a dev harness is Permission (settings.json deny) + a
PreToolUse hook.

## OWASP LLM Top 10 — what to check here
- **Prompt injection (esp. indirect):** product descriptions, merchant uploads, and any
  file/web the agent reads are untrusted and may carry hidden instructions. Most
  dangerous class for this app.
- **Sensitive info disclosure:** never let `.env`, keys, PEM, connection strings, or PII
  be read/pasted. "Sent = unrecoverable."
- **Excessive agency:** scope tool permissions; don't disable the shell for security —
  scope it.
- **Improper output handling:** any model/merchant text rendered to the storefront is
  untrusted → escape before display (XSS/injection).
- **Supply chain:** don't install unverified packages (slopsquatting); treat MCP servers
  as supply chain (`deny mcp__*` then allow selectively).

## settings.json Rules (order matters)
**deny → ask → allow; deny always wins.**
- `deny`: `Read(.env)`, `Read(**/.env)`, `Read(**/*.pem)`, `~/.ssh/**`, `~/.aws/**`,
  destructive bash (`rm -rf`, raw `curl`/`wget`), and **editing the hook/settings
  themselves** (`Edit(.claude/hooks/**)`, `Write(.claude/hooks/**)`).
- `ask`: `git push *`.
- `allow`: safe, scoped project commands only.
- Anchor-path gotcha: absolute deny paths need the double-slash form. Deny does not stop
  a subprocess reading a file — that needs a sandbox.

## PreToolUse Hook — strongest self-controlled guardrail
See `references/pretooluse-hook.md` for the template and the rules (read stdin with
`INPUT=$(cat)`, command at `.tool_input.command`; `exit 2` blocks, `exit 1` still runs;
require `jq` and fail-safe block if missing; use specific patterns like `rm -rf` not bare
`rm`; deny editing the hook itself; test with piped JSON before trusting). PostToolUse
cannot block — use it only for lint/format/audit.

## Code-Review "3 KHÔNG"
1. Don't install unverified packages. 2. Don't trust "clean" input (SQL/path/HTML/shell/
eval). 3. Don't merge without asking "how would an attacker hit this?" Run SAST.

## Output
Write `_workspace/{phase}_security_{scope}.md`: finding · severity · OWASP category ·
concrete exploit path · fix (prefer a deterministic guardrail over a prompt). On
re-review, confirm each prior finding was actually fixed, not just acknowledged.
