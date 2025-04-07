#!/usr/bin/env bash

update-ca-certificates

echo "Replacing all symlinks by the files they link to"

cd /etc/ssl/certs || exit

find . -type l -print0 | while IFS= read -r -d '' file
do
  cp --remove-destination "$(readlink "$file")" "$file"
done

exec "$@"