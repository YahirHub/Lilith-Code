package moduleapi

import "sync"

var (
	catalogMu sync.RWMutex
	catalog   []Definition
)

// Register adds a statically-linked module definition to the process catalog.
// It is intended to be called from module init functions. Validation and
// conflict handling happen when a Registry is constructed so one bad private
// module cannot panic the public core at process startup.
func Register(def Definition) {
	catalogMu.Lock()
	catalog = append(catalog, def)
	catalogMu.Unlock()
}

// Catalog returns an isolated snapshot of every module linked into this build.
func Catalog() []Definition {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	out := make([]Definition, len(catalog))
	copy(out, catalog)
	return out
}
