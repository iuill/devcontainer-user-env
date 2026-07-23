# shellcheck shell=bash

[[ $- == *i* ]] || return
[[ -n ${DEVCONTAINER_CONTEXT:-} || -f /.dockerenv ]] || return
[[ -z ${DEVCONTAINER_PROMPT_LOADED:-} ]] || return

DEVCONTAINER_PROMPT_LOADED=1
export DEVCONTAINER_PROMPT_CONTEXT=DEV

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if command -v starship >/dev/null 2>&1; then
    export STARSHIP_CONFIG="${repository_root}/starship.toml"
    eval "$(starship init bash)"
else
    PS1='\[\e[1;97;40m\] 🐳 \[\e[1;97;44m\] DEV \[\e[0m\] '"${PS1}"
fi

unset repository_root
