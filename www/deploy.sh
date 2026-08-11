#!/usr/bin/env bash
#
# Publish www/ to a static host over ssh.
#
# Deliberately carries no host, user, path or key: this repository is destined
# to be public, and a committed deploy target is an infrastructure disclosure
# that nobody remembers to remove on the day the repo flips. Everything comes
# from the environment, and the script stops rather than guessing.
#
#   BIFROST_WWW_HOST   user@host of the web server            (required)
#   BIFROST_WWW_PATH   absolute docroot on that server        (required)
#   BIFROST_WWW_KEY    ssh identity file                      (optional)
#
# Usage:
#   ./www/deploy.sh              publish
#   ./www/deploy.sh --dry-run    show what would change, touch nothing

set -euo pipefail

# An `[[ ]] && x=1` one-liner returns non-zero when the test fails, which under
# `set -e` exits the script. Hence the if.
DRY_RUN=0
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=1
fi

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

: "${BIFROST_WWW_HOST:?set BIFROST_WWW_HOST, e.g. user@example.com}"
: "${BIFROST_WWW_PATH:?set BIFROST_WWW_PATH, e.g. /var/www/example.com}"

SSH="ssh"
if [[ -n "${BIFROST_WWW_KEY:-}" ]]; then
  SSH="ssh -i ${BIFROST_WWW_KEY}"
fi

# Repository furniture, not site content.
EXCLUDES=(--exclude README.md --exclude deploy.sh --exclude '.DS_Store')

# Relative to the remote home; rsync resolves it there.
STAGE=".bifrost-www-stage"

if [[ "$DRY_RUN" == 1 ]]; then
  echo "==> dry run against ${BIFROST_WWW_HOST}:${BIFROST_WWW_PATH}"
  rsync -avn --delete "${EXCLUDES[@]}" -e "$SSH" \
    "$SRC/" "${BIFROST_WWW_HOST}:${BIFROST_WWW_PATH}/"
  exit 0
fi

echo "==> staging to ${BIFROST_WWW_HOST}"
rsync -av --delete "${EXCLUDES[@]}" -e "$SSH" \
  "$SRC/" "${BIFROST_WWW_HOST}:${STAGE}/"

echo "==> publishing to ${BIFROST_WWW_PATH}"
# Staged first so the swap into the docroot is a single local rsync rather than
# a slow network copy that leaves the site half-written while it runs.
$SSH "$BIFROST_WWW_HOST" \
  DOCROOT="$BIFROST_WWW_PATH" 'bash -euo pipefail -s' <<'REMOTE'
STAGE="$HOME/.bifrost-www-stage"
sudo rsync -a --delete "$STAGE/" "$DOCROOT/"
sudo chown -R www-data:www-data "$DOCROOT"
rm -rf "$STAGE"
REMOTE

echo "==> done"
