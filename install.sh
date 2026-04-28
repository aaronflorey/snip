#!/bin/sh
# install.sh - Install snip (LLM token optimizer)
# Usage: curl -fsSL https://raw.githubusercontent.com/aaronflorey/snip/master/install.sh | sh

set -e

REPO="aaronflorey/snip"
INSTALL_DIR="/usr/local/bin"
FALLBACK_DIR="${HOME}/.local/bin"

# --- helpers ---

info() {
  printf '[snip] %s\n' "$1"
}

error() {
  printf '[snip] ERROR: %s\n' "$1" >&2
  exit 1
}

need_cmd() {
  if ! command -v "$1" > /dev/null 2>&1; then
    error "required command not found: $1"
  fi
}

# --- detect platform ---

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux" ;;
    Darwin*) echo "darwin" ;;
    *)       error "unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo "amd64" ;;
    aarch64|arm64)  echo "arm64" ;;
    *)              error "unsupported architecture: $(uname -m)" ;;
  esac
}

# --- main ---

main() {
  need_cmd curl
  need_cmd tar
  need_cmd uname

  OS="$(detect_os)"
  ARCH="$(detect_arch)"

  info "detected platform: ${OS}/${ARCH}"

  # fetch latest version
  info "fetching latest release..."
  LATEST_TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | head -1 \
    | sed 's/.*"tag_name": *"//;s/".*//')

  if [ -z "${LATEST_TAG}" ]; then
    error "could not determine latest release"
  fi

  VERSION="${LATEST_TAG#v}"
  info "latest version: ${VERSION} (${LATEST_TAG})"

  # build download URL
  # GoReleaser template: snip_<version>_<os>_<arch>.tar.gz
  ARCHIVE="snip_${VERSION}_${OS}_${ARCH}.tar.gz"
  URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/${ARCHIVE}"

  # create temp directory
  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "${TMP_DIR}"' EXIT

  # download
  info "downloading ${URL}"
  curl -fsSL -o "${TMP_DIR}/${ARCHIVE}" "${URL}"

  # extract
  info "extracting..."
  tar xzf "${TMP_DIR}/${ARCHIVE}" -C "${TMP_DIR}"

  if [ ! -f "${TMP_DIR}/snip" ]; then
    error "binary not found in archive"
  fi

  # install
  if [ -d "${INSTALL_DIR}" ] && [ -w "${INSTALL_DIR}" ]; then
    TARGET="${INSTALL_DIR}/snip"
  else
    mkdir -p "${FALLBACK_DIR}"
    TARGET="${FALLBACK_DIR}/snip"
    info "${INSTALL_DIR} is not writable, installing to ${FALLBACK_DIR}"
  fi

  mv "${TMP_DIR}/snip" "${TARGET}"
  chmod +x "${TARGET}"

  # verify
  if ! "${TARGET}" --version > /dev/null 2>&1; then
    info "installed to ${TARGET}"
  else
    INSTALLED_VERSION="$("${TARGET}" --version 2>&1 || true)"
    info "installed ${INSTALLED_VERSION} to ${TARGET}"
  fi

  # check PATH
  case ":${PATH}:" in
    *":$(dirname "${TARGET}"):"*) ;;
    *)
      printf '\n'
      info "WARNING: $(dirname "${TARGET}") is not in your PATH"
      info "Add it with:  export PATH=\"$(dirname "${TARGET}"):\${PATH}\""
      ;;
  esac

  printf '\n'
  info "done! Run 'snip init' to set up your AI coding assistant hook."
}

main
