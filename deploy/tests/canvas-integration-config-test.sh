#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

config_file=deploy/config.example.yaml
proxy_file=deploy/canvas-oreniva.example.conf

grep -Fq 'https://oreniva.com' "$config_file"
grep -Fq 'https://canvas.oreniva.com' "$config_file"
grep -Fq 'frame-ancestors https://oreniva.com' "$proxy_file"
grep -Fq 'server_name canvas.oreniva.com' "$proxy_file"
grep -Fq 'proxy_pass http://127.0.0.1:3000' "$proxy_file"

if grep -Eq 'allowed_origins:[[:space:]]*\[[^]]*\*' "$config_file"; then
  printf '%s\n' 'canvas integration config must not use wildcard CORS origins' >&2
  exit 1
fi

if grep -Fq 'proxy_set_header Authorization' "$proxy_file"; then
  printf '%s\n' 'canvas reverse proxy must not forward or log Authorization explicitly' >&2
  exit 1
fi

printf '%s\n' 'canvas integration config test passed'
