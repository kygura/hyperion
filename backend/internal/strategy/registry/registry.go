// Package registry maps strategy names to constructors. In-process Go
// strategies register from their package's init (builtin does this); the
// stdio adapter (stdio.go) wraps an external process behind the same
// Strategy interface, so the runtime never distinguishes the two.
package registry

import (
	"fmt"
	"sort"
	"sync"

	"github.com/hyperagent/hyperagent/internal/strategy"
)

// Constructor builds a fresh Strategy instance.
type Constructor func() strategy.Strategy

var (
	mu    sync.RWMutex
	ctors = map[string]Constructor{}
)

// Register adds a constructor under name. Registering the same name twice
// panics: two plugins claiming one id is a wiring bug, not a runtime state.
func Register(name string, c Constructor) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := ctors[name]; dup {
		panic(fmt.Sprintf("strategy registry: %q registered twice", name))
	}
	ctors[name] = c
}

// Unregister removes a name (tests).
func Unregister(name string) {
	mu.Lock()
	defer mu.Unlock()
	delete(ctors, name)
}

// Get builds a new instance of the named strategy.
func Get(name string) (strategy.Strategy, error) {
	mu.RLock()
	c, ok := ctors[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("strategy registry: unknown strategy %q", name)
	}
	return c(), nil
}

// Names lists registered names, sorted.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(ctors))
	for n := range ctors {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// All builds one instance per registered strategy, keyed by name.
func All() map[string]strategy.Strategy {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]strategy.Strategy, len(ctors))
	for n, c := range ctors {
		out[n] = c()
	}
	return out
}
