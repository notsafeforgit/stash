#!/usr/bin/env bash
# shellcheck shell=bash

# Native Stash has no bundled Python scraper dependencies. An operator can opt
# into installing an explicit requirements file for their chosen extensions.
install_python_deps() {
  case "${INSTALL_PYTHON_REQUIREMENTS:-false}" in
    false|FALSE|0) return 0 ;;
    true|TRUE|1) ;;
    *)
      error "INSTALL_PYTHON_REQUIREMENTS must be true or false"
      return 1
      ;;
  esac
  if [ ! -f "$PYTHON_REQS" ] || [ ! -s "$PYTHON_REQS" ]; then
    error "INSTALL_PYTHON_REQUIREMENTS requires an explicit nonempty $PYTHON_REQS"
    return 1
  fi
  info "Installing explicitly configured Python requirements from $PYTHON_REQS"
  try_reown_r "$UV_TARGET" || return 1
  try_reown_r "$UV_CACHE_DIR" || return 1
  runas /usr/local/bin/uv-pip requirements "$PYTHON_REQS"
}
