#!/usr/bin/env sh
set -eu

REPOSITORY="${LI_REPOSITORY:-YahirHub/Lilith-Code}"
REQUESTED_VERSION="${LI_VERSION:-${1:-latest}}"
SKIP_TERMUX_PACKAGES="${LI_SKIP_TERMUX_PACKAGES:-0}"

say() { printf '%s\n' "$*"; }
warn() { printf 'Aviso: %s\n' "$*" >&2; }
fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "se requiere '$1'"; }
path_contains() {
  case ":${PATH:-}:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

need uname
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  fail "se requiere curl o wget"
fi

os="$(uname -s)"
[ "$os" = "Linux" ] || fail "este instalador soporta Linux y Termux; en Windows usa install.ps1 o install.cmd"

is_termux=0
case "${PREFIX:-}" in
  */com.termux/files/usr) is_termux=1 ;;
esac
if [ -n "${TERMUX_VERSION:-}" ] || [ -n "${TERMUX_APP_PID:-}" ]; then
  is_termux=1
fi

machine="$(uname -m)"
install_dir=""
installed=0
if [ "$is_termux" -eq 1 ]; then
  case "$machine" in
    aarch64|arm64) asset="li-termux-arm64" ;;
    *) fail "Termux está soportado actualmente en ARM64/AArch64; arquitectura detectada: $machine" ;;
  esac
  [ -n "${PREFIX:-}" ] || fail "Termux no expuso PREFIX; reinicia la app y vuelve a intentar"
  install_dir="$PREFIX/bin"
  [ -d "$install_dir" ] || fail "no existe el directorio de binarios de Termux: $install_dir"
  [ -w "$install_dir" ] || fail "el directorio de binarios de Termux no es escribible: $install_dir"
else
  case "$machine" in
    x86_64|amd64) asset="li-linux-amd64" ;;
    aarch64|arm64) asset="li-linux-arm64" ;;
    armv7l|armv7) asset="li-linux-armv7" ;;
    *) fail "arquitectura no soportada: $machine" ;;
  esac
fi

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
elif command -v openssl >/dev/null 2>&1; then
  actual="$(openssl dgst -sha256 "$tmp/$asset" | awk '{print $NF}')"
else
  fail "se requiere sha256sum, shasum u openssl para verificar la descarga"
fi
[ "$actual" = "$expected" ] || fail "el checksum SHA-256 no coincide"
chmod 0755 "$tmp/$asset"

if [ "$is_termux" -eq 1 ]; then
  if [ "$SKIP_TERMUX_PACKAGES" != "1" ] && command -v pkg >/dev/null 2>&1; then
    missing=""
    command -v git >/dev/null 2>&1 || missing="$missing git"
    command -v rg >/dev/null 2>&1 || missing="$missing ripgrep"
    if [ -n "$missing" ]; then
      say "Instalando dependencias recomendadas de Termux:$missing"
      # shellcheck disable=SC2086
      pkg install -y $missing
    fi
  elif [ "$SKIP_TERMUX_PACKAGES" = "1" ]; then
    warn "se omitió la instalación de git/ripgrep por LI_SKIP_TERMUX_PACKAGES=1"
  fi
else
  # Install only into a directory already present in this shell's PATH. A child
  # installer cannot mutate the parent shell environment, so this guarantees
  # that `li` works immediately without editing or reloading .bashrc.
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
if [ "$is_termux" -eq 1 ]; then
  say "Entorno: Termux ARM64"
fi
say "Ejecuta: li"
