#!/usr/bin/env bash

set -eu

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bashrc="${HOME}/.bashrc"
source_line="source \"${repository_root}/shell/devcontainer.bash\""

touch "$bashrc"
if ! grep -qxF "$source_line" "$bashrc"; then
    printf '\n%s\n' "$source_line" >>"$bashrc"
fi

if [[ ${DEVCONTAINER_USER_ENV_SILENT:-} != 1 ]]; then
    printf 'Installed the DEV prompt in %s\n' "$bashrc"
fi
