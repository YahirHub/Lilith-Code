# Built-in Lilith skills

This directory is reserved for optional built-in Agent Skills embedded in the
`li` binary. Lilith currently ships **without product-specific built-in
skills**: installation, compilation, updating and release procedures belong to
repository documentation and CI, not to the model's skill catalog.

User and project skills continue to work normally from `~/.li/skills`,
`<project>/.li/skills` and the compatible Claude/OpenCode locations. If a
future generic skill is added here, it must be useful across projects and must
not teach the model how to install, compile or publish Lilith itself.
