package loong

import (
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Node is one component instance in the config tree. The kernel only
// reads the skeleton (type/id/children); Config stays opaque and is
// decoded by the component itself (schema-per-component).
type Node struct {
	Type     string    `yaml:"type"`
	ID       string    `yaml:"id,omitempty"`
	Config   yaml.Node `yaml:"config"`
	Children []*Node   `yaml:"children"`

	parent    *Node
	component Component
	handlers  map[string]Handler
}

// envRe matches ${ENV_VAR} placeholders expanded from the environment.
var envRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// LoadTree reads and parses a loong.yaml config tree. ${ENV} placeholders
// in the file are replaced with the corresponding environment variables
// (empty string when unset).
func LoadTree(path string) (*Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = envRe.ReplaceAllFunc(data, func(m []byte) []byte {
		return []byte(os.Getenv(string(m[2 : len(m)-1])))
	})
	var root Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return &root, nil
}
