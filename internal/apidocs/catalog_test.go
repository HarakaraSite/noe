package apidocs

import (
	"strings"
	"testing"
)

func TestCatalogNavigationAndIsolation(t *testing.T) {
	index, err := Lookup("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range Topics() {
		if !strings.Contains(index, "`"+topic.ID+"`") {
			t.Fatalf("topic absent from index: %s", topic.ID)
		}
		overview, err := Lookup(topic.ID, "")
		if err != nil || !strings.HasPrefix(overview, "# ") {
			t.Fatalf("topic %s: %v", topic.ID, err)
		}
		for _, alias := range topic.Aliases {
			text, err := Lookup(alias, "")
			if err != nil || text != overview {
				t.Fatalf("alias %s differs from %s", alias, topic.ID)
			}
		}
		for _, member := range topic.Members {
			text, err := Lookup(topic.ID, member)
			if err != nil || !strings.Contains(text, "\n## "+member+"\n") {
				t.Fatalf("unreachable detail %s.%s: %v", topic.ID, member, err)
			}
		}
	}
	for _, name := range []string{"noe:agent/api", "noe:agent/hooks", "noe:agent/internal", "node:net", "../../instructions.md"} {
		if _, err := Lookup(name, ""); err == nil {
			t.Fatalf("unexpected public topic: %s", name)
		}
	}
	if _, err := Lookup("node:fs", "unlistedMember"); err == nil || !strings.Contains(err.Error(), "does not imply an unsupported API") {
		t.Fatal("missing detail must be distinguished from runtime support")
	}
	topics := Topics()
	originalID := topics[0].ID
	topics[0].ID = "changed"
	if len(topics[0].Members) > 0 {
		topics[0].Members[0] = "changed"
	}
	if Topics()[0].ID != originalID || strings.Contains(strings.Join(Topics()[0].Members, ","), "changed") {
		t.Fatal("caller mutated catalog metadata")
	}
}

func TestMarkdownMemberBoundaries(t *testing.T) {
	body := "# Example\n\nShared lifetime contract.\n\n```md\n## NotAMember\n```\n\n## first\n\nfirst detail\n\n## Handle.close\n\nclose detail\n"
	e, err := parse(topicSpec{ID: "example"}, body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(e.info.Members, ",") != "first,Handle.close" || !strings.Contains(e.overview, "## NotAMember") || strings.Contains(e.details["first"], "close detail") {
		t.Fatalf("invalid detail boundaries: %+v", e)
	}
	for _, body := range []string{"no title", "# Example\n## first\na\n## first\nb", "# Example\n## invalid member\na"} {
		if _, err := parse(topicSpec{ID: "example"}, body); err == nil {
			t.Fatalf("accepted invalid catalog body: %q", body)
		}
	}
}

func TestInvalidCatalogMetadata(t *testing.T) {
	spec := noeTopics()[0]
	for _, specs := range [][]topicSpec{
		{{ID: "missing"}},
		{{ID: "missing", Kind: "module", Summary: "not embedded", File: "absent.md"}},
		{spec, spec},
	} {
		if _, err := newCatalog(specs); err == nil {
			t.Fatal("accepted incomplete or duplicate metadata")
		}
	}
}
