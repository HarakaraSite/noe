// Package apidocs contains the API reference shipped with the executable.
// It performs immutable in-memory lookups and has no runtime or provider dependency.
package apidocs

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed content/*.md
var content embed.FS

type topicSpec struct {
	ID, Kind, Summary, File string
	Aliases                 []string
}

// Topic describes a public reference topic. Members lists the documented details,
// which may be a subset of the runtime's exports (especially for the stdlib).
type Topic struct {
	ID, Kind, Summary string
	Aliases, Members  []string
}

type entry struct {
	info     Topic
	overview string
	details  map[string]string
}

type catalog struct {
	entries map[string]*entry
	ordered []*entry
	index   string
}

var builtin = mustCatalog()

func mustCatalog() *catalog {
	c, err := newCatalog(append(noeTopics(), stdlibTopics()...))
	if err != nil {
		panic("invalid embedded API reference: " + err.Error())
	}
	return c
}

func newCatalog(specs []topicSpec) (*catalog, error) {
	c := &catalog{entries: make(map[string]*entry)}
	for _, spec := range specs {
		if spec.ID == "" || spec.Kind == "" || spec.Summary == "" {
			return nil, fmt.Errorf("incomplete topic metadata: %q", spec.ID)
		}
		data, err := content.ReadFile("content/" + spec.File)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", spec.ID, err)
		}
		e, err := parse(spec, string(data))
		if err != nil {
			return nil, err
		}
		for _, name := range append([]string{spec.ID}, spec.Aliases...) {
			if name == "" || c.entries[name] != nil {
				return nil, fmt.Errorf("duplicate or empty topic/alias %q", name)
			}
			c.entries[name] = e
		}
		c.ordered = append(c.ordered, e)
	}
	sort.Slice(c.ordered, func(i, j int) bool { return c.ordered[i].info.ID < c.ordered[j].info.ID })
	var b strings.Builder
	b.WriteString("# NOE API reference\n\nBundled with this executable. Read a topic, then select a detailed member when needed.\n\nCLI: `noe docs [topic [member]]`\n\nJavaScript: `import { docs } from \"noe:docs\"; console.log(docs(topic, member));`\n\n")
	for _, e := range c.ordered {
		fmt.Fprintf(&b, "- `%s` (%s): %s\n", e.info.ID, e.info.Kind, e.info.Summary)
	}
	c.index = b.String()
	return c, nil
}

// parse uses level-two headings exclusively as detail IDs. Ignore headings
// inside fenced examples, so the authoring format remains ordinary Markdown.
func parse(spec topicSpec, body string) (*entry, error) {
	e := &entry{info: Topic{ID: spec.ID, Kind: spec.Kind, Summary: spec.Summary, Aliases: append([]string(nil), spec.Aliases...)}, details: make(map[string]string)}
	var section strings.Builder
	member := ""
	fence := byte(0)
	fenceLength := 0
	flush := func() {
		text := strings.TrimSpace(section.String()) + "\n"
		if member == "" {
			e.overview = text
		} else {
			e.details[member] = text
		}
		section.Reset()
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) <= 3 && len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			ch := trimmed[0]
			n := 0
			for n < len(trimmed) && trimmed[n] == ch {
				n++
			}
			if fence == 0 && n >= 3 {
				fence, fenceLength = ch, n
			} else if ch == fence && n >= fenceLength && strings.TrimSpace(trimmed[n:]) == "" {
				fence = 0
			}
		} else if fence == 0 && strings.HasPrefix(line, "## ") {
			flush()
			member = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if member == "" || strings.ContainsAny(member, " \t`") || e.details[member] != "" {
				return nil, fmt.Errorf("%s: invalid or duplicate member %q", spec.ID, member)
			}
			e.info.Members = append(e.info.Members, member)
		}
		section.WriteString(line + "\n")
	}
	flush()
	if !strings.HasPrefix(e.overview, "# ") {
		return nil, fmt.Errorf("%s: missing topic title", spec.ID)
	}
	return e, nil
}

// Topics returns a detached description of the public catalog.
func Topics() []Topic {
	result := make([]Topic, 0, len(builtin.ordered))
	for _, e := range builtin.ordered {
		info := e.info
		info.Aliases = append([]string(nil), info.Aliases...)
		info.Members = append([]string(nil), info.Members...)
		result = append(result, info)
	}
	return result
}

// Lookup returns Markdown. An empty topic selects the index. An empty member
// selects the topic overview and the list of available details.
func Lookup(topic, member string) (string, error) {
	if topic == "" && member == "" {
		return builtin.index, nil
	}
	e := builtin.entries[topic]
	if e == nil {
		return "", fmt.Errorf("API reference topic %q not found; use noe docs for available topics", topic)
	}
	if member != "" {
		if detail, ok := e.details[member]; ok {
			return e.overview + "\n" + detail, nil
		}
		return "", fmt.Errorf("API reference detail %q not found in %s; use noe docs %s for available details (a missing detail does not imply an unsupported API)", member, e.info.ID, e.info.ID)
	}
	var b strings.Builder
	b.WriteString(e.overview)
	if len(e.info.Members) > 0 {
		b.WriteString("\n## Detailed members\n\n")
		for _, name := range e.info.Members {
			fmt.Fprintf(&b, "- `%s`\n", name)
		}
		fmt.Fprintf(&b, "\nCLI: `noe docs %s MEMBER`\n\nJavaScript: `docs(%q, \"MEMBER\")`\n", e.info.ID, e.info.ID)
	}
	return b.String(), nil
}
