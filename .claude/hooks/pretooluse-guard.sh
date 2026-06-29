#!/usr/bin/env bash
set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
  echo "pretooluse-guard: jq not found; blocking for safety" >&2
  exit 2
fi

INPUT=$(cat)
TOOL=$(printf '%s' "$INPUT" | jq -r '.tool_name // empty')
CMD=$(printf '%s'  "$INPUT" | jq -r '.tool_input.command // empty')

if [ "$TOOL" = "Bash" ]; then
  case "$CMD" in
    *"rm -rf"*|*"rm -fr"*)
      echo "Blocked: destructive recursive delete." >&2; exit 2 ;;
    *":(){"*|*"mkfs"*|*"dd if="*)
      echo "Blocked: dangerous system command." >&2; exit 2 ;;
  esac
  # Block reading secrets via shell.
  case "$CMD" in
    *".env"*|*".pem"*|*"id_rsa"*|*".aws/credentials"*)
      echo "Blocked: attempt to read a secret file via shell." >&2; exit 2 ;;
  esac
fi

exit 0
