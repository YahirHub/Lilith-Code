#!/usr/bin/env python3
"""Exercise install.sh update paths without network access."""

from __future__ import annotations

import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


REQUIRED_COMMANDS = (
    "awk",
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
        "if [ \"${1:-}\" = version ]; then echo 'Lilith 0.1.1'; "
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
        ["/bin/sh", str(installer), "0.1.1"],
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
    release = root / "release"
    home = root / "home"
    for directory in (prefix / "bin", fakebin, release, home):
        directory.mkdir(parents=True, exist_ok=True)

    write_executable(prefix / "bin" / "li", "#!/bin/sh\necho 'Lilith 0.1.0'\n")
    binary = write_release(release, "li-termux-arm64")
    link_test_commands(fakebin)
    write_fake_curl(fakebin, binary, release / "SHA256SUMS.txt")
    write_executable(
        fakebin / "uname",
        "#!/bin/sh\n"
        "case \"${1:-}\" in -s) echo Linux;; -m) echo aarch64;; *) echo Linux;; esac\n",
    )

    pkg_log = root / "pkg.log"
    write_executable(
        fakebin / "pkg",
        "#!/bin/sh\n"
        f"printf '%s\\n' \"$*\" >> '{pkg_log}'\n"
        f"cat > '{prefix / 'bin' / 'rg'}' <<'EOF'\n#!/bin/sh\nexit 0\nEOF\n"
        f"chmod +x '{prefix / 'bin' / 'rg'}'\n"
        f"cat > '{prefix / 'bin' / 'git'}' <<'EOF'\n#!/bin/sh\nexit 0\nEOF\n"
        f"chmod +x '{prefix / 'bin' / 'git'}'\n",
    )

    env = os.environ.copy()
    env.update(
        {
            "HOME": str(home),
            "PREFIX": str(prefix),
            "TERMUX_VERSION": "0.119",
            "PATH": os.pathsep.join((str(fakebin), str(prefix / "bin"))),
        }
    )
    output = run_installer(installer, repo, env)

    version = subprocess.check_output(
        [str(prefix / "bin" / "li"), "version"], text=True
    ).strip()
    if version != "Lilith 0.1.1":
        raise AssertionError(f"Termux installer did not update li: {version!r}")
    if "install -y git ripgrep" not in pkg_log.read_text(encoding="utf-8"):
        raise AssertionError("Termux installer did not request git/ripgrep")
    if (home / ".bashrc").exists():
        raise AssertionError("Termux installer must not edit .bashrc")
    if "Entorno: Termux ARM64" not in output:
        raise AssertionError("installer did not select its Termux branch")


def simulate_linux(installer: Path, repo: Path, root: Path) -> None:
    targetbin = root / "path-bin"
    fakebin = root / "fakebin"
    release = root / "release"
    home = root / "home"
    for directory in (targetbin, fakebin, release, home):
        directory.mkdir(parents=True, exist_ok=True)

    write_executable(targetbin / "li", "#!/bin/sh\necho 'Lilith 0.1.0'\n")
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
            # The writable destination is already in PATH, so the installed
            # command must work immediately in the same shell environment.
            "PATH": os.pathsep.join((str(targetbin), str(fakebin))),
        }
    )
    output = run_installer(installer, repo, env)

    version = subprocess.check_output(
        [str(targetbin / "li"), "version"], text=True
    ).strip()
    if version != "Lilith 0.1.1":
        raise AssertionError(f"Linux installer did not update li: {version!r}")
    if (home / ".bashrc").exists():
        raise AssertionError("Linux installer must not edit .bashrc")
    if f"Lilith quedó instalado en {targetbin / 'li'}" not in output:
        raise AssertionError("Linux installer did not use an existing PATH directory")


def main() -> None:
    repo = Path(__file__).resolve().parents[2]
    installer = repo / "install.sh"
    with tempfile.TemporaryDirectory(prefix="lilith-install-tests-") as tmp_raw:
        root = Path(tmp_raw)
        simulate_termux(installer, repo, root / "termux")
        simulate_linux(installer, repo, root / "linux")
    print("Linux and Termux install/update simulations passed")


if __name__ == "__main__":
    main()
