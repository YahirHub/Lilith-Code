#!/usr/bin/env sh
set -eu

REPOSITORY="${LI_REPOSITORY:-YahirHub/Lilith-Code}"
REQUESTED_VERSION="${LI_VERSION:-${1:-latest}}"

say() { printf '%s\n' "$*"; }
fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "se requiere '$1'"; }

need uname
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  fail "se requiere curl o wget"
fi

os="$(uname -s)"
[ "$os" = "Linux" ] || fail "este instalador soporta Linux; en Windows usa install.ps1 o install.cmd"

machine="$(uname -m)"
case "$machine" in
  x86_64|amd64) asset="li-linux-amd64" ;;
  aarch64|arm64) asset="li-linux-arm64" ;;
  armv7l|armv7) asset="li-linux-armv7" ;;
  *) fail "arquitectura no soportada: $machine" ;;
esac

if [ "$REQUESTED_VERSION" = "latest" ]; then
  base="https://github.com/$REPOSITORY/releases/latest/download"
  display_version="la versión más reciente"
else
  case "$REQUESTED_VERSION" in v*) tag="$REQUESTED_VERSION" ;; *) tag="v$REQUESTED_VERSION" ;; esac
  base="https://github.com/$REPOSITORY/releases/download/$tag"
  display_version="$tag"
fi

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t lilith-install)"
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT INT TERM

say "Descargando $display_version para $machine..."
fetch "$base/$asset" "$tmp/$asset"
fetch "$base/SHA256SUMS.txt" "$tmp/SHA256SUMS.txt"

expected="$(awk -v name="$asset" '$2 == name || $2 == "*" name {print $1; exit}' "$tmp/SHA256SUMS.txt")"
[ -n "$expected" ] || fail "el release no contiene el checksum de $asset"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
else
  fail "se requiere sha256sum o shasum para verificar la descarga"
fi
[ "$actual" = "$expected" ] || fail "el checksum SHA-256 no coincide"
chmod 0755 "$tmp/$asset"

install_dir=""
for candidate in /usr/local/bin /usr/bin; do
  if [ -d "$candidate" ] && [ -w "$candidate" ]; then
    install_dir="$candidate"
    break
  fi
done

if [ -z "$install_dir" ] && command -v sudo >/dev/null 2>&1; then
  install_dir="/usr/local/bin"
  sudo mkdir -p "$install_dir"
  sudo install -m 0755 "$tmp/$asset" "$install_dir/li.new"
  sudo mv -f "$install_dir/li.new" "$install_dir/li"
else
  if [ -z "$install_dir" ]; then
    old_ifs="$IFS"; IFS=:
    for candidate in $PATH; do
      [ -n "$candidate" ] || continue
      if [ -d "$candidate" ] && [ -w "$candidate" ]; then
        install_dir="$candidate"
        break
      fi
    done
    IFS="$old_ifs"
  fi
  if [ -z "$install_dir" ]; then
    fail "no hay un directorio escribible en PATH y sudo no está disponible; instala sudo o ejecuta el instalador como root"
  fi
  cp "$tmp/$asset" "$install_dir/li.new"
  chmod 0755 "$install_dir/li.new"
  mv -f "$install_dir/li.new" "$install_dir/li"
fi

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *)
    fail "Lilith se instaló en $install_dir, pero ese directorio no pertenece al PATH de esta terminal. Abre una terminal nueva para usar 'li'."
    ;;
esac

hash -r 2>/dev/null || true
say "Lilith quedó instalado en $install_dir/li"
"$install_dir/li" version
say "Ejecuta: li"
