# shellcheck shell=bash

[[ $- == *i* ]] || return
[[ -z ${DEVCONTAINER_CONTEXT:-} ]] || return
[[ ! -f /.dockerenv ]] || return
[[ -z ${DEVCONTAINER_HOST_PROMPT_LOADED:-} ]] || return

DEVCONTAINER_HOST_PROMPT_LOADED=1
export DEVCONTAINER_PROMPT_CONTEXT=HOST

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if command -v starship >/dev/null 2>&1; then
    export STARSHIP_CONFIG="${repository_root}/starship.toml"
    eval "$(starship init bash)"
else
    PS1='\[\e[1;97;40m\] ⚠️ \[\e[1;30;43m\] HOST \[\e[0m\] '"${PS1}"
fi

unset repository_root
