#!/usr/bin/env bash
set -euo pipefail

# Instalador de DNS Watcher (Go) compatible con `curl -fsSL <url>/install.sh | bash`
REPO_URL="${DNS_WATCHER_REPO:-https://github.com/ivanmercedes/dns-watcher}"
BRANCH="${DNS_WATCHER_BRANCH:-main}"

BIN_DIR="$HOME/.local/bin"
BIN_PATH="$BIN_DIR/dns-watcher"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
esac

echo "==> Instalando DNS Watcher (Go) en $BIN_PATH"
mkdir -p "$BIN_DIR"

SCRIPT_DIR=""
if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi

if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/dist/dns-watcher-${OS}-${ARCH}" ]; then
  cp "$SCRIPT_DIR/dist/dns-watcher-${OS}-${ARCH}" "$BIN_PATH"
elif [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/go.mod" ] && command -v go >/dev/null 2>&1; then
  (cd "$SCRIPT_DIR" && go build -trimpath -ldflags="-s -w" -o "$BIN_PATH" .)
else
  if [ -z "$REPO_URL" ]; then
    echo "Error: define DNS_WATCHER_REPO (ej. https://github.com/usuario/dns-watcher) para instalación remota." >&2
    exit 1
  fi
  RELEASE_URL="$REPO_URL/releases/latest/download/dns-watcher-${OS}-${ARCH}"
  RAW_DIST_URL="$REPO_URL/raw/$BRANCH/dist/dns-watcher-${OS}-${ARCH}"
  if curl -fsSL "$RELEASE_URL" -o "$BIN_PATH" 2>/dev/null || curl -fsSL "$RAW_DIST_URL" -o "$BIN_PATH" 2>/dev/null; then
    true
  elif command -v go >/dev/null 2>&1; then
    TMP_DIR="$(mktemp -d)"
    trap 'rm -rf "$TMP_DIR"' EXIT
    curl -fsSL "$REPO_URL/archive/refs/heads/$BRANCH.tar.gz" -o "$TMP_DIR/src.tar.gz"
    tar -xzf "$TMP_DIR/src.tar.gz" -C "$TMP_DIR"
    SRC_ROOT="$(find "$TMP_DIR" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
    (cd "$SRC_ROOT" && go build -trimpath -ldflags="-s -w" -o "$BIN_PATH" .)
  else
    echo "Error: no se encontró binario precompilado ni Go instalado." >&2
    exit 1
  fi
fi

chmod +x "$BIN_PATH"
if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
  echo "==> Agrega $BIN_DIR a tu PATH (ej. export PATH=\"\$HOME/.local/bin:\$PATH\")"
fi

DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/dns-watcher"
if [ -f "$DATA_DIR/domains.txt" ]; then
  "$BIN_PATH" init
else
  echo "==> Importa tus dominios con:"
  echo "    dns-watcher domains import /ruta/a/Dominios.xlsx && dns-watcher init"
fi
echo "==> Instalación completada. Ejecuta: dns-watcher status"
