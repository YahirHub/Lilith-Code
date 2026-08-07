// Package distribution is the compile-time module selection point.
//
// Public Lilith always links the built-ins below. A private downstream branch
// can add another file in this package guarded by a build tag (for example
// //go:build company) that blank-imports private modules. Main never needs to
// know those package paths, which keeps merges one-way and low-conflict.
package distribution

import (
	_ "github.com/lilith/li/internal/modules/builtin/modules"
	_ "github.com/lilith/li/internal/modules/builtin/rewind"
	_ "github.com/lilith/li/internal/modules/builtin/skills"
)
