#!/usr/bin/env bash

set -eu

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bin_directory="${XDG_BIN_HOME:-$HOME/.local/bin}"
lib_directory="${HOME}/.local/share/devcontainer-user-env/bin"
unit_directory="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

"${repository_root}/bin/build-screenshot-web"

mkdir -p "$bin_directory" "$lib_directory" "$unit_directory"
install -d -m 700 "${HOME}/screenshots"
install -m 755 "${repository_root}/build/screenshot-web" "${lib_directory}/screenshot-web"
ln -sfn "${lib_directory}/screenshot-web" "${bin_directory}/screenshot-web"
ln -sfn "${repository_root}/systemd/screenshot-web.service" \
    "${unit_directory}/screenshot-web.service"

if command -v systemctl >/dev/null 2>&1; then
    systemctl --user daemon-reload || {
        printf 'warning: systemd user manager is unavailable; skipped daemon-reload\n' >&2
    }
fi

printf 'Installed screenshot-web to %s\n' "${bin_directory}/screenshot-web"
printf '%s\n' \
    'To start it now and at login:' \
    '  systemctl --user enable --now screenshot-web.service' \
    'To publish it inside your tailnet:' \
    '  tailscale serve --bg --yes 3939'
