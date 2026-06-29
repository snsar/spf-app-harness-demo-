# PreToolUse Hook — Template & Rules

The PreToolUse hook is the strongest guardrail you control directly: it runs before a
tool executes and can block it. Use it to enforce what AGENTS.md can only advise.

## Iron Rules
- **Read input from stdin**, not env vars: `INPUT=$(cat)`. The shell command is at
  `.tool_input.command`.
- **`exit 2` blocks** the tool call. **`exit 1` does NOT block** (the tool still runs) —
  it only logs. `exit 0` allows.
- **Require `jq`; fail safe.** If `jq` is missing, block (exit 2) rather than allow.
- **Specific patterns only.** Match `rm -rf`, not bare `rm` (which matches
  `npm run format`, `terraform`, etc.). Over-broad patterns break normal work and get
  disabled — then you have no guardrail.
- **Deny editing the hook itself** via settings.json (`Edit/Write(.claude/hooks/**)`),
  so the agent cannot neuter its own guardrail.
- **Test with piped JSON before trusting it** (see bottom).

## Events
| Event | Can block? | Use for |
|-------|-----------|---------|
| PreToolUse | yes (`exit 2`) | safety + security blocks (mandatory) |
| UserPromptSubmit | yes | input-layer checks |
| Stop | yes | completion gates |
| PostToolUse | **no** | lint / format / audit only |

## Template (`.claude/hooks/pretooluse-guard.sh`)
```bash
#!/usr/bin/env bash
# PreToolUse guard: block destructive bash and secret reads. exit 2 = block.
set -euo pipefail

# Fail safe: no jq -> block.
if ! command -v jq >/dev/null 2>&1; then
  echo "pretooluse-guard: jq not found; blocking for safety" >&2
  exit 2
fi

INPUT=$(cat)
TOOL=$(printf '%s' "$INPUT" | jq -r '.tool_name // empty')
CMD=$(printf '%s'  "$INPUT" | jq -r '.tool_input.command // empty')

# Destructive bash patterns (specific, not bare verbs).
if [ "$TOOL" = "Bash" ]; then
  case "$CMD" in
    *"rm -rf"*|*"rm -fr"*)
      echo "Blocked: destructive recursive delete." >&2; exit 2 ;;
    *":(){"*|*"mkfs"*|*"dd if="*)
      echo "Blocked: dangerous system command." >&2; exit 2 ;;
  esac
  # Block reading secrets via shell (deny rules don't cover subprocess reads).
  case "$CMD" in
    *".env"*|*".pem"*|*"id_rsa"*|*".aws/credentials"*)
      echo "Blocked: attempt to read a secret file via shell." >&2; exit 2 ;;
  esac
fi

exit 0
```

## settings.json wiring
```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash",
        "hooks": [ { "type": "command", "command": ".claude/hooks/pretooluse-guard.sh" } ] }
    ]
  }
}
```

## Test before trusting
```bash
echo '{"tool_name":"Bash","tool_input":{"command":"rm -rf /tmp/x"}}' \
  | .claude/hooks/pretooluse-guard.sh; echo "exit=$?"   # expect blocked, exit=2

echo '{"tool_name":"Bash","tool_input":{"command":"npm run format"}}' \
  | .claude/hooks/pretooluse-guard.sh; echo "exit=$?"   # expect allowed, exit=0
```
