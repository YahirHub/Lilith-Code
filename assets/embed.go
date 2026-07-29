// Package assets contains files that are compiled into the Lilith binary.
package assets

import (
	"embed"
	"io/fs"
)

// embeddedSkills contains the built-in Agent Skills shipped with Lilith.
// Add each skill under assets/skills/<skill-name>/SKILL.md. Any references or
// other Markdown resources below that directory are embedded as well.
//
//go:embed skills
var embeddedSkills embed.FS

// embeddedAgents contains built-in Claude-compatible subagent definitions.
//
//go:embed agents
var embeddedAgents embed.FS

// SkillsFS returns the embedded assets/skills tree with "skills" removed from
// the visible path. The returned filesystem is read-only.
func SkillsFS() fs.FS {
	sub, err := fs.Sub(embeddedSkills, "skills")
	if err != nil {
		panic(err)
	}
	return sub
}

// AgentsFS returns the embedded assets/agents tree with "agents" removed.
func AgentsFS() fs.FS {
	sub, err := fs.Sub(embeddedAgents, "agents")
	if err != nil {
		panic(err)
	}
	return sub
}
