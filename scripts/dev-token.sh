#!/usr/bin/env bash
# Create an API token directly in a development database and print it.
#
# Usage: scripts/dev-token.sh DB PREFIX [EMAIL]
#
# The token is bound to the user with EMAIL, or to the first user. The database
# must exist, so start the server once first. Development only: this bypasses
# the admin page at /-/admin/tokens.
set -euo pipefail

db=${1:?usage: dev-token.sh DB PREFIX [EMAIL]}
prefix=${2:?usage: dev-token.sh DB PREFIX [EMAIL]}
email=${3:-}

command -v sqlite3 >/dev/null || { echo "sqlite3 not found" >&2; exit 1; }
[ -f "$db" ] || { echo "$db not found; start the server once to create it" >&2; exit 1; }

if [ -n "$email" ]; then
	user_id=$(sqlite3 "$db" "SELECT id FROM user WHERE email = '${email//\'/\'\'}';")
else
	user_id=$(sqlite3 "$db" "SELECT id FROM user ORDER BY id LIMIT 1;")
fi
[ -n "$user_id" ] || { echo "no such user; register one first" >&2; exit 1; }

# Same format and hash as db.CreateAPIToken: only the SHA-256 is stored.
token="gw_$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=')"
hash=$(printf %s "$token" | shasum -a 256 | cut -d' ' -f1)
sqlite3 "$db" "INSERT INTO api_tokens (user_id, label, token_hash, write_prefix, created_at)
	VALUES ($user_id, 'dev', '$hash', '${prefix//\'/\'\'}', datetime('now'));"
echo "$token"
