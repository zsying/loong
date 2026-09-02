package commands

import (
	"fmt"
	"html"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zsying/loong/components/cli"

	"github.com/zsying/loong"
)

// Tree implements the `tree` subcommand: it prints a project's config
// tree, annotating each node with its declared services and config
// keys where the component type is known to the registry.
type Tree struct {
	loong.Base
}

func (c *Tree) Run(ctx *loong.Scope) error {
	args, _ := ctx.Args.([]string)
	pos := cli.Positional(args)
	if len(pos) == 0 {
		return fmt.Errorf("cli tree: missing config path (usage: cli tree [--html|-h] <config.yaml>)")
	}
	root, err := loong.LoadTree(pos[0])
	if err != nil {
		return err
	}
	meta := catalogIndex()
	if cli.HasFlag(args, "h", "html") {
		fmt.Print(renderTreeHTML(root, meta))
	} else {
		fmt.Print(renderTree(root, meta))
	}
	return nil
}

func init() {
	loong.RegisterComponent("cli.tree", func() loong.Component { return &Tree{} },
		loong.WithDesc("show a project config tree with services and config"),
	)
}

// catalogIndex builds a type -> metadata index from the registry.
func catalogIndex() map[string]loong.ComponentMeta {
	idx := make(map[string]loong.ComponentMeta)
	for _, m := range loong.Components() {
		idx[m.Type] = m
	}
	return idx
}

// nodeLabel renders the identity and annotation of one node.
func nodeLabel(n *loong.Node, meta map[string]loong.ComponentMeta) string {
	label := n.Type
	if n.ID != "" && n.ID != n.Type {
		label += " (" + n.ID + ")"
	}
	var annos []string
	if m, ok := meta[n.Type]; ok && m.Service {
		annos = append(annos, "service")
	}
	if n.Lazy {
		annos = append(annos, "lazy")
	}
	if len(annos) > 0 {
		label += " [" + strings.Join(annos, " · ") + "]"
	}
	return label
}

// configText flattens a node's config block into one line "k=v · k=v".
func configText(n *loong.Node) string {
	if n.Config.IsZero() {
		return ""
	}
	data, err := yaml.Marshal(&n.Config)
	if err != nil {
		return ""
	}
	var pairs []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pairs = append(pairs, strings.ReplaceAll(line, ": ", "="))
	}
	return strings.Join(pairs, " · ")
}

// renderTree prints the tree with box-drawing indentation.
func renderTree(root *loong.Node, meta map[string]loong.ComponentMeta) string {
	var b strings.Builder
	var walk func(n *loong.Node, prefix string, isLast bool)
	walk = func(n *loong.Node, prefix string, isLast bool) {
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		label := nodeLabel(n, meta)
		fmt.Fprintln(&b, prefix+connector+label)
		if cfg := configText(n); cfg != "" {
			fmt.Fprintln(&b, prefix+"    "+"│   "+cfg)
		}
		childPrefix := prefix + "    "
		if !isLast {
			childPrefix = prefix + "│   "
		}
		for i, c := range n.Children {
			walk(c, childPrefix, i == len(n.Children)-1)
		}
	}
	// Root without connector.
	fmt.Fprintln(&b, nodeLabel(root, meta))
	if cfg := configText(root); cfg != "" {
		fmt.Fprintln(&b, "    "+cfg)
	}
	for i, c := range root.Children {
		walk(c, "", i == len(root.Children)-1)
	}
	return b.String()
}

// renderTreeHTML prints the tree as nested lists.
func renderTreeHTML(root *loong.Node, meta map[string]loong.ComponentMeta) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>loong tree</title></head><body>")
	var walk func(n *loong.Node)
	walk = func(n *loong.Node) {
		b.WriteString("<li>" + html.EscapeString(nodeLabel(n, meta)))
		if cfg := configText(n); cfg != "" {
			b.WriteString(" <code>" + html.EscapeString(cfg) + "</code>")
		}
		if len(n.Children) > 0 {
			b.WriteString("<ul>")
			for _, c := range n.Children {
				walk(c)
			}
			b.WriteString("</ul>")
		}
		b.WriteString("</li>")
	}
	b.WriteString("<ul>")
	walk(root)
	b.WriteString("</ul></body></html>")
	return b.String()
}
