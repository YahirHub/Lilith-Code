// Package distribution is the compile-time module selection point.
//
// Public Lilith links only the core modules below. A private downstream can add
// another file in this package guarded by a build tag (for example company)
// that blank-imports modules/company/** without editing this public file.
package distribution

import (
	_ "github.com/lilith/li/modules/core/agents"
	_ "github.com/lilith/li/modules/core/compaction"
	_ "github.com/lilith/li/modules/core/config"
	_ "github.com/lilith/li/modules/core/fork"
	_ "github.com/lilith/li/modules/core/goal"
	_ "github.com/lilith/li/modules/core/help"
	_ "github.com/lilith/li/modules/core/mcp"
	_ "github.com/lilith/li/modules/core/memory"
	_ "github.com/lilith/li/modules/core/mode"
	_ "github.com/lilith/li/modules/core/modules"
	_ "github.com/lilith/li/modules/core/plugins"
	_ "github.com/lilith/li/modules/core/project"
	_ "github.com/lilith/li/modules/core/providers"
	_ "github.com/lilith/li/modules/core/rewind"
	_ "github.com/lilith/li/modules/core/session"
	_ "github.com/lilith/li/modules/core/shell"
	_ "github.com/lilith/li/modules/core/skills"
)
