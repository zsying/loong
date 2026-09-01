package loong

import (
	"os"
	"regexp"
	"sync"

	"gopkg.in/yaml.v3"
)

// Node is one component instance in the config tree. The kernel only
// reads the skeleton (type/id/lazy/children); Config stays opaque and
// is decoded by the component itself (schema-per-component).
//
// Lazy nodes are registered during assembly but not activated: they
// are instantiated on demand through Get[T]() (service components) or
// Kernel.Activate. The act* fields guard that on-demand activation
// against concurrent callers.
type Node struct {
	Type     string    `yaml:"type"`
	ID       string    `yaml:"id,omitempty"`
	Lazy     bool      `yaml:"lazy,omitempty"`
	Config   yaml.Node `yaml:"config"`
	Children []*Node   `yaml:"children"`

	parent    *Node
	component Component
	handlers  map[string]Handler

	actMu   sync.Mutex
	actDone bool
	actErr  error
}

// envRe matches ${ENV_VAR} placeholders expanded from the environment.
var envRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Parse parses a config tree from raw yaml bytes, expanding ${ENV}
// placeholders before parsing (text-level, same semantics as
// LoadTree). It is the entry point for embedding a config tree into a
// program — e.g. a CLI that ships its component tree inline via
// "go:embed".
func Parse(data []byte) (*Node, error) {
	data = envRe.ReplaceAllFunc(data, func(m []byte) []byte {
		return []byte(os.Getenv(string(m[2 : len(m)-1])))
	})
	var root Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return &root, nil
}

// LoadTree reads and parses a loong.yaml config tree from a file.
// ${ENV} placeholders in the file are replaced with the corresponding
// environment variables (empty string when unset). Expansion is
// text-level and happens before YAML parsing, so values containing
// YAML-significant characters (:, #, quotes, ...) must be quoted in
// the config file.
func LoadTree(path string) (*Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}
