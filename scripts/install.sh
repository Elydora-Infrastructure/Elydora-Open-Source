#!/usr/bin/env sh
set -e

# ───────────────────────────────────────────────────────────
#  Elydora Open Source — One-click deployment
# ───────────────────────────────────────────────────────────

printf '\n'
printf '  ╔══════════════════════════════════════════╗\n'
printf '  ║       Elydora Open Source Installer       ║\n'
printf '  ║       Self-hosted AI audit platform       ║\n'
printf '  ╚══════════════════════════════════════════╝\n'
printf '\n'

# ── helpers ────────────────────────────────────────────────

fail() { printf '\n[ERROR] %s\n' "$1" >&2; exit 1; }
info() { printf '[INFO]  %s\n' "$1"; }
ok()   { printf '[OK]    %s\n' "$1"; }

# Platform-aware sed in-place (macOS needs '')
sedi() {
  if [ "$(uname)" = "Darwin" ]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

# ── 1. check prerequisites ────────────────────────────────

info "Checking prerequisites..."

command -v docker >/dev/null 2>&1 \
  || fail "docker is not installed. See https://docs.docker.com/get-docker/"

docker compose version >/dev/null 2>&1 \
  || fail "docker compose (v2) is not available. See https://docs.docker.com/compose/install/"

command -v openssl >/dev/null 2>&1 \
  || fail "openssl is not installed."

command -v curl >/dev/null 2>&1 \
  || fail "curl is not installed."

ok "All prerequisites satisfied."

# ── 2. generate .env ───────────────────────────────────────

cd "$(dirname "$0")/.."

if [ -f .env ]; then
  info "Existing .env found — skipping secret generation."
else
  info "Generating .env from .env.example..."

  [ -f .env.example ] || fail ".env.example not found in project root."

  cp .env.example .env

  # Generate secrets
  PG_PASS=$(openssl rand -hex 24)
  MINIO_PASS=$(openssl rand -hex 24)
  BETTER_AUTH_SECRET=$(openssl rand -hex 32)
  SIGNING_KEY=$(openssl genpkey -algorithm ed25519 2>/dev/null \
    | openssl pkcs8 -topk8 -nocrypt -outform DER 2>/dev/null \
    | tail -c 32 \
    | base64 \
    | tr '+/' '-_' \
    | tr -d '=\n')

  [ -n "$PG_PASS" ]      || fail "Failed to generate Postgres password."
  [ -n "$MINIO_PASS" ]    || fail "Failed to generate MinIO password."
  [ -n "$BETTER_AUTH_SECRET" ] || fail "Failed to generate Better Auth secret."
  [ -n "$SIGNING_KEY" ]   || fail "Failed to generate Ed25519 signing key."

  # Replace placeholders
  sedi "s|POSTGRES_PASSWORD=GENERATE_ME|POSTGRES_PASSWORD=${PG_PASS}|" .env
  sedi "s|elydora:GENERATE_ME@postgres|elydora:${PG_PASS}@postgres|" .env
  sedi "s|MINIO_ROOT_PASSWORD=GENERATE_ME|MINIO_ROOT_PASSWORD=${MINIO_PASS}|" .env
  sedi "s|MINIO_SECRET_KEY=GENERATE_ME|MINIO_SECRET_KEY=${MINIO_PASS}|" .env
  sedi "s|BETTER_AUTH_SECRET=GENERATE_ME|BETTER_AUTH_SECRET=${BETTER_AUTH_SECRET}|" .env
  sedi "s|ELYDORA_SIGNING_KEY=GENERATE_ME|ELYDORA_SIGNING_KEY=${SIGNING_KEY}|" .env

  chmod 600 .env

  ok "Generated .env with fresh secrets (permissions 0600)."
fi

# ── 3. docker compose up ──────────────────────────────────

info "Starting services with docker compose..."
docker compose up -d

# ── 4. wait for API health ────────────────────────────────

info "Waiting for API to become healthy (up to 60 s)..."

elapsed=0
while [ "$elapsed" -lt 60 ]; do
  if curl -sf http://localhost:8787/v1/health >/dev/null 2>&1; then
    break
  fi
  sleep 2
  elapsed=$((elapsed + 2))
done

if [ "$elapsed" -ge 60 ]; then
  fail "API did not become healthy within 60 seconds. Run 'docker compose logs api' to diagnose."
fi

# ── 5. done ───────────────────────────────────────────────

printf '\n'
printf '  ╔══════════════════════════════════════════╗\n'
printf '  ║         Elydora is running!               ║\n'
printf '  ╠══════════════════════════════════════════╣\n'
printf '  ║  Console:       http://localhost:3000     ║\n'
printf '  ║  API:           http://localhost:8787     ║\n'
printf '  ║  MinIO Console: http://localhost:9001     ║\n'
printf '  ╚══════════════════════════════════════════╝\n'
printf '\n'
