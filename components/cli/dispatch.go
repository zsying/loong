package cli

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/zsying/loong"
)

// dispatch walks the command path (e.g. ["config", "show", "theme"])
// and activates each node in order. The command hierarchy mirrors the
// component tree: a node with children is a command group, a node
// without children is a leaf command. Only groups accept a next-level
// command; arguments after the leaf command are the command's own and
// are handed to it through the master's Context. Invoking a group
// directly (no subcommand) is a usage error. Returns the exit code.
func dispatch(k *loong.Kernel, args []string) int {
	if len(args) == 0 {
		return usage(k)
	}

	// Resolve the command path and split off the leaf command's own
	// arguments.
	var path, leafArgs []string
	for i, name := range args {
		info, err := k.Node(name)
		if err != nil {
			slog.Error("unknown command", "cmd", name)
			return 1
		}
		if len(info.Children) == 0 {
			path = args[:i+1]
			leafArgs = args[i+1:]
			break
		}
		if i == len(args)-1 {
			slog.Error("command group requires a subcommand", "cmd", name,
				"subcommands", groupNames(info.Children))
			return 2
		}
	}

	// Hand the leaf arguments to the command via the master's Context.
	if err := setContextArgs(k, leafArgs); err != nil {
		slog.Error("cli master", "err", err)
		return 1
	}

	for _, name := range path {
		if err := k.Activate(name); err != nil {
			slog.Error("command failed", "cmd", name, "err", err)
			return 1
		}
	}
	return 0
}

// setContextArgs replaces the master's Context arguments with the
// leaf command's own arguments.
func setContextArgs(k *loong.Kernel, args []string) error {
	root, err := k.Root()
	if err != nil {
		return err
	}
	comp, err := k.Component(root.ID)
	if err != nil {
		return err
	}
	master, ok := comp.(*Component)
	if !ok {
		return fmt.Errorf("cli: root %q is not a cli master", root.ID)
	}
	master.Ctx.SetArgs(args)
	return nil
}

// usage prints the command tree under the root and returns exit code 2.
func usage(k *loong.Kernel) int {
	root, err := k.Root()
	if err != nil {
		slog.Error("no command tree", "err", err)
		return 2
	}
	fmt.Println("usage: <command>")
	printCommands(root.Children, 1)
	return 2
}

// printCommands renders a node's children with indentation, recursing
// into command groups.
func printCommands(cmds []*loong.NodeInfo, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, c := range cmds {
		if len(c.Children) > 0 {
			fmt.Printf("%s%s <%s>\n", indent, c.ID, groupNames(c.Children))
			continue
		}
		fmt.Printf("%s%s\n", indent, c.ID)
	}
}

func groupNames(children []*loong.NodeInfo) string {
	names := make([]string, 0, len(children))
	for _, c := range children {
		names = append(names, c.ID)
	}
	return strings.Join(names, ", ")
}
