#!/usr/bin/env bash
set -euo pipefail

API="${API:-http://localhost:8080}"
USERNAME="${USERNAME:-admin}"
PASSWORD="${PASSWORD:-admin}"
WORKSPACE="${WORKSPACE:-demo}"
FUNCTION="${FUNCTION:-e2e-$(date +%s)}"

say() { printf "\033[1;36m==> %s\033[0m\n" "$*"; }

say "login as ${USERNAME}"
TOKEN=$(curl -fsS -X POST "${API}/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${USERNAME}\",\"password\":\"${PASSWORD}\"}" \
  | jq -r .token)

auth() { curl -fsS -H "Authorization: Bearer ${TOKEN}" "$@"; }

say "ensure workspace ${WORKSPACE}"
auth -X POST "${API}/api/v1/workspaces" \
  -H 'Content-Type: application/json' \
  -d "{\"slug\":\"${WORKSPACE}\",\"name\":\"${WORKSPACE}\"}" >/dev/null 2>&1 || true

say "create function ${FUNCTION}"
auth -X POST "${API}/api/v1/functions" \
  -H 'Content-Type: application/json' \
  -H "X-Workspace: ${WORKSPACE}" \
  -d "$(jq -n --arg name "$FUNCTION" '{
    name: $name,
    runtime: "python",
    deploy_type: "managed",
    public: true,
    code: "def handler(request):\n    return {\"echo\": request.get(\"body\", \"\")}\n"
  }')" >/dev/null

say "wait for ready (up to 90s)"
for i in $(seq 1 45); do
  STATUS=$(auth -H "X-Workspace: ${WORKSPACE}" "${API}/api/v1/functions/${FUNCTION}" | jq -r .status)
  printf "  [%2d] status=%s\n" "$i" "$STATUS"
  if [[ "$STATUS" == "ready" ]]; then break; fi
  if [[ "$STATUS" == "failed" ]]; then echo "deploy failed"; exit 1; fi
  sleep 2
done
[[ "$STATUS" == "ready" ]] || { echo "timed out waiting for ready"; exit 1; }

say "invoke via authenticated proxy"
auth -X POST "${API}/api/v1/functions/${FUNCTION}/invoke" \
  -H 'Content-Type: application/json' \
  -H "X-Workspace: ${WORKSPACE}" \
  -d '{"hello":"world"}' | jq .

say "invoke via public path"
curl -fsS -X POST "${API}/fn/${WORKSPACE}/${FUNCTION}" \
  -H 'Content-Type: application/json' \
  -d '{"hello":"public"}' | jq .

say "cleanup"
auth -X DELETE -H "X-Workspace: ${WORKSPACE}" "${API}/api/v1/functions/${FUNCTION}" >/dev/null

say "ok"
