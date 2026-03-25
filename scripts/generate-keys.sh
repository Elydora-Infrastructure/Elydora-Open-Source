#!/bin/sh
# Generates an Ed25519 signing key and a random Better Auth secret for initial setup.
# Requires: openssl
set -e

echo "=== Elydora Key Generation ==="
echo ""

# Generate Ed25519 private key and export as base64url
SIGNING_KEY=$(openssl genpkey -algorithm ed25519 2>/dev/null \
  | openssl pkcs8 -topk8 -nocrypt -outform DER 2>/dev/null \
  | tail -c 32 \
  | base64 \
  | tr '+/' '-_' \
  | tr -d '=\n')

# Generate 32-byte random Better Auth secret as hex
BETTER_AUTH_SECRET=$(openssl rand -hex 32)

echo "Add these to your .env file:"
echo ""
echo "ELYDORA_SIGNING_KEY=${SIGNING_KEY}"
echo "BETTER_AUTH_SECRET=${BETTER_AUTH_SECRET}"
echo ""
echo "WARNING: Store these values securely. Do not commit them to version control."
