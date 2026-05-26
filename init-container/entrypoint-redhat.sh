#!/usr/bin/env sh

TMP_CERTS_DIR="${TMP_CERTS_DIR:-/tmp/certs}"

update-ca-trust extract --output $TMP_CERTS_DIR

exec "$@"