# shellcheck shell=bash

[[ $- == *i* ]] || return
[[ -z ${DEVCONTAINER_CONTEXT:-} ]] || return
[[ ! -f /.dockerenv ]] || return
[[ -z ${DEVCONTAINER_HOST_PROMPT_LOADED:-} ]] || return

export DEVCONTAINER_HOST_PROMPT_LOADED=1
PS1='\[\e[1;97;41m\] HOST \[\e[0m\] '"${PS1}"
