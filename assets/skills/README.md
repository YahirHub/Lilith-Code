# Built-in Lilith skills

This directory contains generic Agent Skills embedded in every `li` binary.
Built-in skills use the same runtime, bounded resource readers and precedence
rules as user/project skills; they are not a second prompt system.

Currently bundled:

- `ponytail-development`: professional software-project methodology focused on
  persistent context, secure simplicity, testing, documentation and Git-based
  delivery.

Users can enable or disable skills globally and individually from `/config >
Skills`. A user or project skill with the same `name` still overrides the
built-in copy. Installation, compilation, updating and release procedures for
Lilith itself remain repository documentation and CI responsibilities rather
than product-specific model skills.
