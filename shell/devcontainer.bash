# shellcheck shell=bash

[[ $- == *i* ]] || return
[[ -n ${DEVCONTAINER_CONTEXT:-} || -f /.dockerenv ]] || return
[[ -z ${DEVCONTAINER_PROMPT_LOADED:-} ]] || return

export DEVCONTAINER_PROMPT_LOADED=1
PS1='\[\e[1;97;44m\] DEV \[\e[0m\] '"${PS1}"
