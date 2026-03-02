#!/bin/sh
# Creates the MinIO bucket for Elydora evidence storage.
# Requires the MinIO client (mc) and the following env vars:
#   MINIO_ENDPOINT, MINIO_ACCESS_KEY, MINIO_SECRET_KEY, MINIO_BUCKET
set -e

: "${MINIO_ENDPOINT:=http://localhost:9000}"
: "${MINIO_ACCESS_KEY:=elydora}"
: "${MINIO_SECRET_KEY:=elydora-minio-secret-change-me}"
: "${MINIO_BUCKET:=elydora-evidence}"

mc alias set elydora "${MINIO_ENDPOINT}" "${MINIO_ACCESS_KEY}" "${MINIO_SECRET_KEY}"
mc mb --ignore-existing "elydora/${MINIO_BUCKET}"
echo "Bucket '${MINIO_BUCKET}' is ready."
