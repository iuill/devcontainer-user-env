#!/usr/bin/env bash

set -eu

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bin_directory="${XDG_BIN_HOME:-$HOME/.local/bin}"
bashrc="${HOME}/.bashrc"
source_line="source \"${repository_root}/shell/host.bash\""

mkdir -p "$bin_directory"
ln -sfn "${repository_root}/bin/dc" "${bin_directory}/dc"

touch "$bashrc"
if ! grep -qxF "$source_line" "$bashrc"; then
    printf '\n%s\n' "$source_line" >>"$bashrc"
fi

printf 'Installed dc to %s\n' "${bin_directory}/dc"
printf 'Installed the HOST prompt in %s\n' "$bashrc"
printf 'Open a new shell or run: source %q\n' "$bashrc"
