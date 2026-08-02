#!/usr/bin/env sh
set -eu

REPOSITORY="${LI_REPOSITORY:-YahirHub/Lilith-Code}"
REQUESTED_VERSION="${LI_VERSION:-${1:-latest}}"
TERMUX_SOURCE_DIR="${LI_TERMUX_SOURCE_DIR:-${HOME:-}/.local/share/lilith/source}"

say() { printf '%s\n' "$*"; }
fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "se requiere '$1'"; }
path_contains() {
  case ":${PATH:-}:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

need uname
os="$(uname -s)"
[ "$os" = "Linux" ] || fail "este instalador soporta Linux y Termux; en Windows usa install.ps1 o install.cmd"

is_termux=0
case "${PREFIX:-}" in
  */com.termux/files/usr|*/com.termux.api/files/usr) is_termux=1 ;;
esac
if [ -n "${TERMUX_VERSION:-}" ] || [ -n "${TERMUX_APP_PID:-}" ]; then
  is_termux=1
fi

machine="$(uname -m)"
tmp="$(mktemp -d 2>/dev/null || mktemp -d -t lilith-install)"
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT INT TERM

if [ "$is_termux" -eq 1 ]; then
  case "$machine" in
    aarch64|arm64) ;;
    *) fail "Termux está soportado actualmente en ARM64/AArch64; arquitectura detectada: $machine" ;;
  esac
  [ -n "${PREFIX:-}" ] || fail "Termux no expuso PREFIX; reinicia la app y vuelve a intentar"
  need pkg

  say "Preparando compilación nativa para Termux ARM64..."
  pkg install -y git golang ripgrep
  need git
  need go

  repository_url="${LI_TERMUX_REPOSITORY_URL:-https://github.com/$REPOSITORY.git}"
  if [ "$REQUESTED_VERSION" = "latest" ]; then
    ref="$(git ls-remote --refs --tags "$repository_url" 'refs/tags/v*' 2>/dev/null | awk '{sub("refs/tags/", "", $2); print $2}' | sort -V | tail -n 1)"
    if [ -z "$ref" ]; then
      ref="${LI_REPOSITORY_REF:-main}"
      display_version="$ref"
    else
      display_version="$ref"
    fi
  else
    case "$REQUESTED_VERSION" in v*) ref="$REQUESTED_VERSION" ;; *) ref="v$REQUESTED_VERSION" ;; esac
    display_version="$ref"
  fi

  say "Clonando Lilith $display_version..."
  git clone --depth 1 --branch "$ref" "$repository_url" "$tmp/source"
  commit="$(git -C "$tmp/source" rev-parse --short HEAD 2>/dev/null || printf 'none')"
  say "Compilando con Go de Termux..."
  (
    cd "$tmp/source"
    CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build \
      -trimpath -buildvcs=false \
      -ldflags="-s -w -X main.commit=$commit" \
      -o "$tmp/li" ./cmd/li
  )
  chmod 0755 "$tmp/li"

  install_dir="$PREFIX/bin"
  [ -d "$install_dir" ] || fail "no existe el directorio de binarios de Termux: $install_dir"
  [ -w "$install_dir" ] || fail "el directorio de binarios de Termux no es escribible: $install_dir"
  cp "$tmp/li" "$install_dir/li.new"
  chmod 0755 "$install_dir/li.new"
  mv -f "$install_dir/li.new" "$install_dir/li"

  [ -n "$TERMUX_SOURCE_DIR" ] || fail "no se pudo determinar el directorio para conservar el código fuente"
  source_parent="$(dirname "$TERMUX_SOURCE_DIR")"
  mkdir -p "$source_parent"
  rm -rf "$TERMUX_SOURCE_DIR.new"
  mv "$tmp/source" "$TERMUX_SOURCE_DIR.new"
  rm -rf "$TERMUX_SOURCE_DIR"
  mv "$TERMUX_SOURCE_DIR.new" "$TERMUX_SOURCE_DIR"

  hash -r 2>/dev/null || true
  say "Lilith quedó compilado e instalado en $install_dir/li"
  say "Código fuente: $TERMUX_SOURCE_DIR"
  "$install_dir/li" version
  say "Entorno: Termux ARM64"
  say "Ejecuta: li"
  exit 0
fi

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  fail "se requiere curl o wget"
fi

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

say "Descargando $display_version para $machine..."
fetch "$base/$asset" "$tmp/$asset"
fetch "$base/SHA256SUMS.txt" "$tmp/SHA256SUMS.txt"

expected="$(awk -v name="$asset" '$2 == name || $2 == "*" name {print $1; exit}' "$tmp/SHA256SUMS.txt")"
[ -n "$expected" ] || fail "el release no contiene el checksum de $asset"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
elif command -v openssl >/dev/null 2>&1; then
  actual="$(openssl dgst -sha256 "$tmp/$asset" | awk '{print $NF}')"
else
  fail "se requiere sha256sum, shasum u openssl para verificar la descarga"
fi
[ "$actual" = "$expected" ] || fail "el checksum SHA-256 no coincide"
chmod 0755 "$tmp/$asset"

install_dir=""
installed=0
for candidate in /usr/local/bin /usr/bin; do
  if path_contains "$candidate" && [ -d "$candidate" ] && [ -w "$candidate" ]; then
    install_dir="$candidate"
    break
  fi
done

if [ -z "$install_dir" ]; then
  old_ifs="$IFS"; IFS=:
  for candidate in ${PATH:-}; do
    [ -n "$candidate" ] || continue
    if [ -d "$candidate" ] && [ -w "$candidate" ]; then
      install_dir="$candidate"
      break
    fi
  done
  IFS="$old_ifs"
fi

if [ -z "$install_dir" ] && command -v sudo >/dev/null 2>&1; then
  for candidate in /usr/local/bin /usr/bin; do
    if path_contains "$candidate"; then
      install_dir="$candidate"
      break
    fi
  done
  [ -n "$install_dir" ] || fail "sudo está disponible, pero /usr/local/bin y /usr/bin no pertenecen al PATH actual"
  sudo mkdir -p "$install_dir"
  sudo install -m 0755 "$tmp/$asset" "$install_dir/li.new"
  sudo mv -f "$install_dir/li.new" "$install_dir/li"
  installed=1
fi

[ -n "$install_dir" ] || fail "no hay un directorio escribible en PATH y sudo no está disponible"
if [ "$installed" -eq 0 ]; then
  [ -w "$install_dir" ] || fail "el directorio de instalación no es escribible: $install_dir"
  cp "$tmp/$asset" "$install_dir/li.new"
  chmod 0755 "$install_dir/li.new"
  mv -f "$install_dir/li.new" "$install_dir/li"
fi

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) fail "Lilith se instaló en $install_dir, pero ese directorio no pertenece al PATH de esta terminal" ;;
esac

hash -r 2>/dev/null || true
say "Lilith quedó instalado en $install_dir/li"
"$install_dir/li" version
say "Ejecuta: li"
