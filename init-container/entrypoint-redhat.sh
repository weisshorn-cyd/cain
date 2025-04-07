#!/usr/bin/env sh

mkdir -p /etc/pki/ca-trust/extracted/openssl
mkdir -p /etc/pki/ca-trust/extracted/pem
mkdir -p /etc/pki/ca-trust/extracted/java
mkdir -p /etc/pki/ca-trust/extracted/edk2

exec update-ca-trust "$@"