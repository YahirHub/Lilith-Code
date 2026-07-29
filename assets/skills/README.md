# Built-in Lilith skills

Place built-in Agent Skills in this directory before compiling Lilith:

```text
assets/skills/
└── my-skill/
    ├── SKILL.md
    └── references/
        └── notes.md
```

Everything below `assets/skills` is embedded in the `li` binary by Go's
`embed` package. At runtime Lilith exposes these skills through the same
`list_skills`, `skill_read`, `skill_search` and `skill_files` tools used for
normal skills.

Precedence is intentionally low: a skill with the same `name` in
`~/.li/skills` overrides the built-in copy, and a project skill in
`<project>/.li/skills` overrides both. This lets a user replace or customize a
bundled skill without rebuilding Lilith.

Built-in skills are intended mainly for Markdown instructions/references. The
runtime still supports other resource files because embedded skills are
materialized into Lilith's private cache before the existing skill tools read
them.
