#!/usr/bin/env bash
set -euo pipefail

# This repo is checked out on the server it deploys to, so a deploy is a local
# build-and-restart — the same shape as the bookings repo next door.
#
# **One checkout, more than one wiki.** The same binary serves
# wiki.verftet.info and arkiv.hellenes.it; what separates them is a systemd
# unit, a port, a page folder and a name, and all of that is in an instance
# file under deploy/. The alternative was a second fork of the whole app, and
# then every fix would have had to be made twice forever.
#
#   ./deploy.sh                    build and restart the Verftet wiki
#   ./deploy.sh --pull             git pull first, then deploy (for GitHub edits)
#   ./deploy.sh --setup            install the systemd unit (first time only)
#   INSTANCE=arkiv ./deploy.sh     the same, for the private archive
#
# The pages are a separate repository cloned into <DIR>/pages. A deploy never
# touches it: the documents are not part of this build.

usage() {
    echo "usage: [INSTANCE=<name>] $0 [--setup|--pull]" >&2
    echo "instances: $(cd "$(dirname "$0")/deploy" && ls *.conf 2>/dev/null | sed 's/\.conf$//' | tr '\n' ' ')" >&2
    exit 2
}

ACTION=""
for arg in "$@"; do
    case "$arg" in
        --setup)   ACTION="setup" ;;
        --pull)    ACTION="pull" ;;
        -h|--help) usage ;;
        *)         echo "unknown argument: $arg" >&2; usage ;;
    esac
done

# Which wiki this is. The default is the one that was here first, so every
# command anybody has in their shell history still means what it meant.
INSTANCE="${INSTANCE:-marksheets}"
CONF="deploy/${INSTANCE}.conf"
if [[ ! -f "$CONF" ]]; then
    echo "unknown instance: ${INSTANCE} (no ${CONF})" >&2
    usage
fi
# SERVICE, DIR, PAGES_REMOTE, HEALTH_URL and UNIT_TEMPLATE come from here.
# shellcheck source=deploy/marksheets.conf
source "$CONF"

PAGES="${DIR}/pages"
BINARY="${DIR}/marksheets"
BACKUP="${DIR}/marksheets.prev"
UNIT="/etc/systemd/system/${SERVICE}.service"

# First-time setup: create the directory, clone the pages, install the unit.
if [[ "$ACTION" == "setup" ]]; then
    if [[ -e "$UNIT" ]]; then
        echo "ERROR: ${UNIT} already exists." >&2
        echo "Refusing to overwrite it. Edit it in place instead:" >&2
        echo "  sudo systemctl edit --full ${SERVICE}" >&2
        exit 1
    fi

    echo "-> Creating ${DIR}..."
    sudo mkdir -p "${DIR}"
    sudo chown "$(id -un):$(id -gn)" "${DIR}"

    if [[ ! -d "${PAGES}/.git" ]]; then
        echo "-> Cloning the pages repository into ${PAGES}..."
        git -c credential.helper='!gh auth git-credential' \
            clone "${PAGES_REMOTE}" "${PAGES}"
        # Publishing from the app runs `git push` as this user with no terminal
        # to prompt at, so the clone carries its own credential helper.
        git -C "${PAGES}" config credential.helper '!gh auth git-credential'
    fi

    echo "-> Installing ${UNIT}..."
    sudo cp "$UNIT_TEMPLATE" "$UNIT"
    # The unit ends up holding the Pocket ID client secret. systemd reads it as
    # root, so nothing needs it world-readable.
    sudo chmod 600 "$UNIT"
    sudo systemctl daemon-reload
    sudo systemctl enable "${SERVICE}"

    echo
    echo "-> Unit installed, but AUTH_CLIENT_ID and AUTH_CLIENT_SECRET are blank."
    echo "   Fill them in before starting. With either one empty the app does"
    echo "   not refuse to boot — it runs as one local user and serves every"
    echo "   page to anyone who reaches it:"
    echo "     sudo systemctl edit --full ${SERVICE}"
    echo "   Then run ./deploy.sh"
    exit 0
fi

# Text edits are made on GitHub, so a deploy usually wants that commit first.
if [[ "$ACTION" == "pull" ]]; then
    echo "-> Pulling latest from origin..."
    git -c credential.helper='!gh auth git-credential' pull --ff-only origin main
fi

echo "-> Building..."
# Build to a temp path first so a compile error leaves the running binary alone.
TMP_BINARY="$(mktemp)"
trap 'rm -f "$TMP_BINARY"' EXIT
go build -ldflags="-s -w" -o "$TMP_BINARY" ./cmd/marksheets/

if [[ -e "$BINARY" ]]; then
    echo "-> Backing up current binary to ${BACKUP}..."
    cp "$BINARY" "$BACKUP"
fi

# The binary can't be overwritten while it's executing ("Text file busy"), so
# the service has to stop first. That means a second or two of downtime.
echo "-> Restarting ${SERVICE}..."
sudo systemctl stop "${SERVICE}"
cp "$TMP_BINARY" "$BINARY"
chmod +x "$BINARY"
sudo systemctl start "${SERVICE}"

echo "-> Waiting for health check..."
for _ in $(seq 1 10); do
    sleep 1
    if curl -fsS -o /dev/null "$HEALTH_URL"; then
        echo "Done — ${SERVICE} is serving."
        exit 0
    fi
done

echo >&2
echo "ERROR: ${SERVICE} did not answer ${HEALTH_URL} after 10s." >&2
if [[ -e "$BACKUP" ]]; then
    echo "-> Rolling back to the previous binary..." >&2
    sudo systemctl stop "${SERVICE}"
    cp "$BACKUP" "$BINARY"
    sudo systemctl start "${SERVICE}"
    echo "-> Rolled back. Check: sudo journalctl -u ${SERVICE} -n 50" >&2
fi
exit 1
