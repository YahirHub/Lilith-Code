#!/usr/bin/env python3
"""Exercise Linux binary updates and native Termux source builds without network."""

from __future__ import annotations

import hashlib
import os
from pathlib import Path
import shutil
import re
import subprocess
import tempfile

REPO_ROOT = Path(__file__).resolve().parents[2]
VERSION_MATCH = re.search(
    r'const\s+Current\s*=\s*"([^"]+)"',
    (REPO_ROOT / "internal" / "version" / "version.go").read_text(encoding="utf-8"),
)
if VERSION_MATCH is None:
    raise RuntimeError("unable to read internal/version/version.go")
CURRENT_VERSION = VERSION_MATCH.group(1)
OLD_VERSION = "0.1.2"

REQUIRED_COMMANDS = (
    "awk",
    "sort",
    "tail",
    "dirname",
    "mkdir",
    "sha256sum",
    "cp",
    "chmod",
    "mv",
    "rm",
    "mktemp",
    "cat",
    "printf",
)


def write_executable(path: Path, content: str) -> None:
    path.write_text(content, encoding="utf-8")
    path.chmod(0o755)


def link_test_commands(fakebin: Path) -> None:
    for command in REQUIRED_COMMANDS:
        source = shutil.which(command)
        if source is None:
            raise RuntimeError(f"required test command is missing: {command}")
        (fakebin / command).symlink_to(source)


def write_release(release: Path, asset: str) -> Path:
    binary = release / asset
    write_executable(
        binary,
        "#!/bin/sh\n"
        f"if [ \"${{1:-}}\" = version ]; then echo 'Lilith {CURRENT_VERSION}'; "
        "else echo 'new lilith'; fi\n",
    )
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    (release / "SHA256SUMS.txt").write_text(
        f"{digest}  {asset}\n", encoding="utf-8"
    )
    return binary


def write_fake_curl(fakebin: Path, binary: Path, checksums: Path) -> None:
    write_executable(
        fakebin / "curl",
        "#!/bin/sh\n"
        "out=''\nurl=''\n"
        "while [ $# -gt 0 ]; do\n"
        "  case \"$1\" in\n"
        "    -o) out=\"$2\"; shift 2;;\n"
        "    -*) shift;;\n"
        "    *) url=\"$1\"; shift;;\n"
        "  esac\n"
        "done\n"
        f"case \"$url\" in\n"
        f"  */{binary.name}) cp '{binary}' \"$out\";;\n"
        f"  */SHA256SUMS.txt) cp '{checksums}' \"$out\";;\n"
        "  *) echo \"unexpected URL: $url\" >&2; exit 2;;\n"
        "esac\n",
    )


def run_installer(installer: Path, repo: Path, env: dict[str, str]) -> str:
    completed = subprocess.run(
        ["/bin/sh", str(installer), CURRENT_VERSION],
        cwd=repo,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        check=True,
    )
    return completed.stdout


def simulate_termux(installer: Path, repo: Path, root: Path) -> None:
    prefix = root / "data" / "data" / "com.termux" / "files" / "usr"
    fakebin = root / "fakebin"
    home = root / "home"
    source_fixture = root / "source-fixture"
    source_store = home / ".local" / "share" / "lilith" / "source"
    for directory in (prefix / "bin", fakebin, home, source_fixture / "cmd" / "li"):
        directory.mkdir(parents=True, exist_ok=True)

    (source_fixture / "go.mod").write_text("module github.com/lilith/li\n", encoding="utf-8")
    (source_fixture / "cmd" / "li" / "main.go").write_text("package main\nfunc main() {}\n", encoding="utf-8")
    write_executable(prefix / "bin" / "li", f"#!/bin/sh\necho 'Lilith {OLD_VERSION}'\n")
    link_test_commands(fakebin)
    write_executable(
        fakebin / "uname",
        "#!/bin/sh\n"
        "case \"${1:-}\" in -s) echo Linux;; -m) echo aarch64;; *) echo Linux;; esac\n",
    )

    pkg_log = root / "pkg.log"
    write_executable(
        fakebin / "pkg",
        "#!/bin/sh\n"
        f"printf '%s\\n' \"$*\" >> '{pkg_log}'\n",
    )
    git_log = root / "git.log"
    write_executable(
        fakebin / "git",
        "#!/bin/sh\n"
        f"fixture='{source_fixture}'\n"
        f"printf '%s\\n' \"$*\" >> '{git_log}'\n"
        "case \"${1:-}\" in\n"
        "  clone)\n"
        "    for last do :; done\n"
        "    mkdir -p \"$last\"\n"
        "    cp -R \"$fixture/.\" \"$last/\"\n"
        "    mkdir -p \"$last/.git\"\n"
        "    ;;\n"
        "  -C) printf '0123456\\n';;\n"
        "  *) echo \"unexpected git invocation: $*\" >&2; exit 2;;\n"
        "esac\n",
    )
    go_log = root / "go.log"
    write_executable(
        fakebin / "go",
        "#!/bin/sh\n"
        f"printf '%s\n' \"$*\" >> '{go_log}'\n"
        "if [ \"${1:-}\" = env ] && [ \"${2:-}\" = GOOS ]; then echo android; exit 0; fi\n"
        "out=''\n"
        "while [ $# -gt 0 ]; do\n"
        "  case \"$1\" in -o) out=\"$2\"; shift 2;; *) shift;; esac\n"
        "done\n"
        "[ -n \"$out\" ] || { echo 'missing -o' >&2; exit 2; }\n"
        "cat > \"$out\" <<'SCRIPT'\n"
        "#!/bin/sh\n"
        f"if [ \"${{1:-}}\" = version ]; then echo 'Lilith {CURRENT_VERSION}'; else echo 'new lilith'; fi\n"
        "SCRIPT\n"
        "chmod +x \"$out\"\n",
    )

    env = os.environ.copy()
    env.update(
        {
            "HOME": str(home),
            "PREFIX": str(prefix),
            "TERMUX_VERSION": "0.119",
            "LI_TERMUX_SOURCE_DIR": str(source_store),
            "PATH": os.pathsep.join((str(fakebin), str(prefix / "bin"))),
        }
    )
    output = run_installer(installer, repo, env)

    version = subprocess.check_output(
        [str(prefix / "bin" / "li"), "version"], text=True
    ).strip()
    if version != f"Lilith {CURRENT_VERSION}":
        raise AssertionError(f"Termux installer did not rebuild li: {version!r}")
    if "install -y git golang ripgrep" not in pkg_log.read_text(encoding="utf-8"):
        raise AssertionError("Termux installer did not request git/golang/ripgrep")
    if not (source_store / "go.mod").exists():
        raise AssertionError("Termux installer did not preserve the cloned source")
    if (home / ".bashrc").exists():
        raise AssertionError("Termux installer must not edit .bashrc")
    if "-tags=grammar_set_core" not in go_log.read_text(encoding="utf-8"):
        raise AssertionError("Termux build did not embed the core grammar set")
    git_invocations = git_log.read_text(encoding="utf-8").splitlines()
    clone_invocations = [line for line in git_invocations if line.startswith("clone ")]
    if len(clone_invocations) != 1:
        raise AssertionError(f"Termux installer must perform one clone: {git_invocations!r}")
    clone = clone_invocations[0]
    for required in ("--depth 1", "--single-branch", "--no-tags"):
        if required not in clone:
            raise AssertionError(f"Termux shallow clone is missing {required}: {clone!r}")
    if "--branch" in clone or any(line.startswith("ls-remote ") for line in git_invocations):
        raise AssertionError(f"Termux installer must not resolve or pin tags/branches: {git_invocations!r}")
    if "Clonando la versión más reciente de Lilith" not in output:
        raise AssertionError("installer did not clone the repository default branch")
    if "Compilando con Go de Termux" not in output:
        raise AssertionError("installer did not use its native Termux build branch")


def simulate_linux(installer: Path, repo: Path, root: Path) -> None:
    targetbin = root / "path-bin"
    fakebin = root / "fakebin"
    release = root / "release"
    home = root / "home"
    for directory in (targetbin, fakebin, release, home):
        directory.mkdir(parents=True, exist_ok=True)

    write_executable(targetbin / "li", f"#!/bin/sh\necho 'Lilith {OLD_VERSION}'\n")
    binary = write_release(release, "li-linux-amd64")
    link_test_commands(fakebin)
    write_fake_curl(fakebin, binary, release / "SHA256SUMS.txt")
    write_executable(
        fakebin / "uname",
        "#!/bin/sh\n"
        "case \"${1:-}\" in -s) echo Linux;; -m) echo x86_64;; *) echo Linux;; esac\n",
    )

    env = os.environ.copy()
    env.pop("PREFIX", None)
    env.pop("TERMUX_VERSION", None)
    env.pop("TERMUX_APP_PID", None)
    env.update(
        {
            "HOME": str(home),
            "PATH": os.pathsep.join((str(targetbin), str(fakebin))),
        }
    )
    output = run_installer(installer, repo, env)

    version = subprocess.check_output(
        [str(targetbin / "li"), "version"], text=True
    ).strip()
    if version != f"Lilith {CURRENT_VERSION}":
        raise AssertionError(f"Linux installer did not update li: {version!r}")
    if (home / ".bashrc").exists():
        raise AssertionError("Linux installer must not edit .bashrc")
    if f"Lilith quedó instalado en {targetbin / 'li'}" not in output:
        raise AssertionError("Linux installer did not use an existing PATH directory")


def main() -> None:
    repo = REPO_ROOT
    installer = repo / "install.sh"
    with tempfile.TemporaryDirectory(prefix="lilith-install-tests-") as tmp_raw:
        root = Path(tmp_raw)
        simulate_termux(installer, repo, root / "termux")
        simulate_linux(installer, repo, root / "linux")
    print("Linux update and native Termux build simulations passed")


if __name__ == "__main__":
    main()
