#!/usr/bin/env python3
"""Generate deterministic release notes from Git commits."""

from __future__ import annotations

import argparse
import subprocess
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Commit:
    sha: str
    subject: str


GROUPS = (
    ("✨ Mejoras", ("agregar", "añadir", "implementar", "mejorar", "incorporar", "soportar", "permitir", "crear")),
    ("🐛 Correcciones", ("corregir", "arreglar", "solucionar", "evitar", "reparar", "fix")),
    ("📚 Documentación", ("documentar", "actualizar readme", "actualizar documentación", "docs")),
    ("🧰 Mantenimiento", ("refactor", "limpiar", "actualizar", "ajustar", "reorganizar", "migrar", "test", "prueba")),
)


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def commits_for_range(revision_range: str) -> list[Commit]:
    raw = git("log", "--reverse", "--format=%H%x00%s", revision_range)
    commits: list[Commit] = []
    for line in raw.splitlines():
        if "\x00" not in line:
            continue
        sha, subject = line.split("\x00", 1)
        subject = subject.strip()
        if subject:
            commits.append(Commit(sha=sha, subject=subject))
    return commits


def group_for(subject: str) -> str:
    normalized = subject.casefold().strip()
    for title, prefixes in GROUPS:
        if normalized.startswith(prefixes):
            return title
    return "📦 Otros cambios"


def render(tag: str, previous_tag: str, repository: str, commits: list[Commit]) -> str:
    grouped: dict[str, list[Commit]] = {}
    for commit in commits:
        grouped.setdefault(group_for(commit.subject), []).append(commit)

    lines = [f"# Lilith {tag}", ""]
    if previous_tag:
        lines.extend([
            f"Cambios desde **{previous_tag}** hasta **{tag}**.",
            "",
        ])
    else:
        lines.extend(["Primera versión publicada de Lilith.", ""])

    for title, _ in GROUPS:
        entries = grouped.get(title, [])
        if not entries:
            continue
        lines.extend([f"## {title}", ""])
        for commit in entries:
            lines.append(f"- {commit.subject} ([`{commit.sha[:7]}`](https://github.com/{repository}/commit/{commit.sha}))")
        lines.append("")

    other = grouped.get("📦 Otros cambios", [])
    if other:
        lines.extend(["## 📦 Otros cambios", ""])
        for commit in other:
            lines.append(f"- {commit.subject} ([`{commit.sha[:7]}`](https://github.com/{repository}/commit/{commit.sha}))")
        lines.append("")

    lines.extend(["## Instalación y actualización", "", "### Linux", "", "```bash", f"curl -fsSL https://github.com/{repository}/releases/latest/download/install.sh | bash", "```", "", "### Windows (PowerShell)", "", "```powershell", f"irm https://github.com/{repository}/releases/latest/download/install.ps1 | iex", "```", ""])

    if previous_tag:
        lines.extend([
            f"**Comparación completa:** https://github.com/{repository}/compare/{previous_tag}...{tag}",
            "",
        ])
    return "\n".join(lines)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--tag", required=True)
    parser.add_argument("--previous-tag", default="")
    parser.add_argument("--repository", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    revision_range = f"{args.previous_tag}..HEAD" if args.previous_tag else "HEAD"
    commits = commits_for_range(revision_range)
    Path(args.output).write_text(
        render(args.tag, args.previous_tag, args.repository, commits),
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
