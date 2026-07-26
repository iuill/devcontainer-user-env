#!/usr/bin/env bash

set -euo pipefail

script_path="$(readlink -f "${BASH_SOURCE[0]}")"
repository_root="$(cd "$(dirname "$script_path")" && pwd)"
bin_directory="${XDG_BIN_HOME:-$HOME/.local/bin}"
lib_directory="${HOME}/.local/share/devcontainer-user-env/bin"
unit_directory="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

"${repository_root}/bin/build-agent-inbox"

mkdir -p "$bin_directory" "$lib_directory" "$unit_directory"
install -d -m 700 "${HOME}/agent-inbox"
install -m 755 "${repository_root}/build/agent-inbox" "${lib_directory}/agent-inbox"
ln -sfn "${lib_directory}/agent-inbox" "${bin_directory}/agent-inbox"
ln -sfn "${repository_root}/systemd/agent-inbox.service" \
    "${unit_directory}/agent-inbox.service"

if command -v systemctl >/dev/null 2>&1; then
    systemctl --user daemon-reload || {
        printf 'warning: systemd user manager is unavailable; skipped daemon-reload\n' >&2
    }
fi

printf 'Installed agent-inbox to %s\n' "${bin_directory}/agent-inbox"
printf '%s\n' \
    'To start it now and at login:' \
    '  systemctl --user enable --now agent-inbox.service' \
    'To publish it inside your tailnet:' \
    '  tailscale serve --bg --yes 3939'
