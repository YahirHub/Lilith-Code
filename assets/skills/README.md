# Built-in Lilith skills

This directory contains generic Agent Skills embedded in every `li` binary.
Built-in skills use the same runtime, bounded resource readers and precedence
rules as user/project skills; they are not a second prompt system.

Currently bundled:

- `ponytail-development`: professional software-project methodology focused on
  persistent context, secure simplicity, testing, documentation and Git-based
  delivery.
- `git-github`: modular Git/GitHub operations for local history, remotes, safe
  history rewriting, repository cleanup, pull requests, Actions and releases.
- `docker-development`: modular Docker/Compose guidance for builds, runtime,
  networking, storage, security, debugging, registries and cleanup.
- `frontend-development`: modular web-development workflow with mandatory real
  browser verification and isolated page auditing through the bundled frontend
  browser subagent.

Users can enable or disable skills globally and individually from `/config >
Skills`. A user or project skill with the same `name` still overrides the
built-in copy. Installation, compilation, updating and release procedures for
Lilith itself remain repository documentation and CI responsibilities rather
than product-specific model skills.

Large built-in skills should keep `SKILL.md` as a routing/index document and put
detailed procedures under `references/`. The runtime embeds those resources and
lets the model discover/read only the module needed through `skill_files`,
`skill_search` and bounded `skill_read` calls.
