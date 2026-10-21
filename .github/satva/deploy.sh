#!/usr/bin/env bash
# Deploy one image tag to the live Obot app in Coolify, then health-check; roll back to the previous tag on failure.
# Used by .github/workflows/satva.yml (deploy job and manual rollback). Env:
#   COOLIFY_TOKEN (secret)  TAG (image tag, e.g. sha-1a2b3c4)
#   optional: COOLIFY_API APP_UUID SITE IMAGE
set -euo pipefail
API=${COOLIFY_API:-https://dev.satva.solutions/api/v1}
APP=${APP_UUID:-h488o408480cow8cgo88go04}
SITE=${SITE:-https://obot.satva.xyz}
IMAGE=${IMAGE:-ghcr.io/satva-git/obot-satva}
# Runs inside the CURRENT container before the swap; the dump lands on the /data volume.
BACKUP_CMD="pg_dump -U obot -f /data/obot-pre-deploy.sql obot"
: "${COOLIFY_TOKEN:?}" "${TAG:?}"

api() { curl -sSf -m 30 -H "Authorization: Bearer $COOLIFY_TOKEN" -H 'Content-Type: application/json' "$@"; }
jget() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }
health() { [ "$(curl -s -o /dev/null -m 10 -w '%{http_code}' "$SITE/api/healthz")" = 200 ]; }
set_image() {
  api -X PATCH "$API/applications/$APP" -d "$(python3 -c 'import json,sys; print(json.dumps({
    "docker_registry_image_name": sys.argv[1], "docker_registry_image_tag": sys.argv[2],
    "pre_deployment_command": sys.argv[3]}))' "$1" "$2" "$BACKUP_CMD")" >/dev/null
}
deploy() {
  local d s=unknown
  d=$(api "$API/deploy?uuid=$APP&force=false" | jget '["deployments"][0]["deployment_uuid"]')
  echo "coolify deployment $d"
  for _ in $(seq 90); do
    sleep 10
    s=$(api "$API/deployments/$d" | jget '.get("status")')
    echo "  $s"
    case $s in finished | failed | cancelled*) break ;; esac
  done
  [ "$s" = finished ] || return 1
  for _ in $(seq 30); do health && return 0; sleep 10; done
  echo "health check failed"; return 1
}

app=$(api "$API/applications/$APP")
prev_img=$(echo "$app" | jget '["docker_registry_image_name"]')
prev_tag=$(echo "$app" | jget '["docker_registry_image_tag"]')
echo "live: $prev_img:$prev_tag -> deploying: $IMAGE:$TAG"
[ "$(api "$API/deployments" | jget '.__len__()')" = 0 ] || { echo "a deployment is already running"; exit 1; }
health || echo "WARNING: live is not healthy before deploy"

set_image "$IMAGE" "$TAG"
if deploy; then echo "DEPLOYED $IMAGE:$TAG - $SITE/api/healthz is 200"; exit 0; fi

echo "ROLLING BACK to $prev_img:$prev_tag"
set_image "$prev_img" "$prev_tag"
deploy && echo "rolled back, healthy" || echo "ROLLBACK UNHEALTHY - open Coolify now"
exit 1
