package main

import (
	"fmt"
	"html"
	"os"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/zsying/loong"
)

// listCmd implements `loong list`: it prints the component catalog.
type listCmd struct {
	loong.Base
}

func (c *listCmd) Run(*loong.Scope) error {
	infos := loong.Components()
	if slices.Contains(os.Args[2:], "--html") {
		fmt.Print(renderListHTML(infos))
	} else {
		fmt.Print(renderListText(infos))
	}
	return nil
}

func init() {
	loong.RegisterComponent("cli.list", func() loong.Component { return &listCmd{} },
		loong.WithDesc("list registered components (services, config, events)"),
	)
}

// renderListText formats the catalog as a fixed-width table, sorted
// by type name for stable output.
func renderListText(infos []loong.ComponentMeta) string {
	sorted := append([]loong.ComponentMeta(nil), infos...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Type < sorted[j].Type })
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TYPE\tSERVICE\tMODE\tCONFIG KEYS\tDESC")
	for _, m := range sorted {
		svc := "-"
		if m.Service {
			svc = "service"
		}
		mode := "-"
		if m.Service {
			if m.Eager {
				mode = "eager"
			} else {
				mode = "lazy"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", m.Type, svc, mode, configKeys(m), m.Desc)
	}
	_ = tw.Flush()
	return b.String()
}

// renderListHTML formats the catalog as an HTML table, sorted by type
// name for stable output.
func renderListHTML(infos []loong.ComponentMeta) string {
	sorted := append([]loong.ComponentMeta(nil), infos...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Type < sorted[j].Type })
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>loong components</title></head><body>")
	b.WriteString("<h1>loong components</h1><table border=\"1\" cellpadding=\"4\" cellspacing=\"0\">")
	b.WriteString("<tr><th>type</th><th>service</th><th>mode</th><th>config keys</th><th>desc</th></tr>")
	for _, m := range sorted {
		mode := "-"
		if m.Service {
			if m.Eager {
				mode = "eager"
			} else {
				mode = "lazy"
			}
		}
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%v</td><td>%s</td><td>%s</td><td>%s</td></tr>",
			html.EscapeString(m.Type), m.Service, mode, configKeys(m), html.EscapeString(m.Desc))
	}
	b.WriteString("</table></body></html>")
	return b.String()
}

// configKeys joins a component's declared config keys, marking
// optional ones with a trailing "?".
func configKeys(m loong.ComponentMeta) string {
	if len(m.ConfigFields) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(m.ConfigFields))
	for _, f := range m.ConfigFields {
		if f.Optional {
			keys = append(keys, f.Name+"?")
		} else {
			keys = append(keys, f.Name)
		}
	}
	return strings.Join(keys, ", ")
}
