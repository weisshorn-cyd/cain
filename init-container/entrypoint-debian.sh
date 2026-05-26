#!/usr/bin/env bash

TMP_CERTS_DIR="${TMP_CERTS_DIR:-/tmp/certs}"

mkdir -p $TMP_CERTS_DIR

update-ca-certificates --fresh --etccertsdir $TMP_CERTS_DIR

echo "Replacing all symlinks by the files they link to"

cd $TMP_CERTS_DIR || exit

find . -type l -print0 | while IFS= read -r -d '' file
do
  cp --remove-destination "$(readlink "$file")" "$file"
done

exec "$@"