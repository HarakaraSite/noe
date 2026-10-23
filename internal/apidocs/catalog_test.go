package apidocs

import (
	"strings"
	"testing"
	"unicode/utf8"
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
		if len(topic.Members) > 0 {
			if !strings.Contains(overview, "\n## Detailed members\n") {
				t.Fatalf("topic %s lost its member navigation", topic.ID)
			}
			for _, member := range topic.Members {
				if !strings.Contains(overview, "- `"+member+"`\n") {
					t.Fatalf("topic %s omits member navigation for %s", topic.ID, member)
				}
			}
		}
		if topic.Kind == "module" || topic.Kind == "global" || topic.ID == "stdlib" {
			if !strings.Contains(overview, "Type declarations:") || !strings.Contains(overview, "Availability:") {
				t.Fatalf("topic %s must distinguish reference types from runtime availability", topic.ID)
			}
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
			if !strings.Contains(text, topic.Summary) || strings.Contains(text, "## Detailed members") {
				t.Fatalf("detail %s.%s must keep only its compact topic context", topic.ID, member)
			}
			for _, other := range topic.Members {
				if other != member && strings.Contains(text, "\n## "+other+"\n") {
					t.Fatalf("detail %s.%s includes unrelated member %s", topic.ID, member, other)
				}
			}
			if topic.Kind == "module" || topic.Kind == "global" {
				if !strings.Contains(text, "Type declarations:") || !strings.Contains(text, "Availability:") {
					t.Fatalf("detail %s.%s lost type or runtime availability context", topic.ID, member)
				}
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

func TestMemberDetailsKeepRunJSExecutionAndImportContext(t *testing.T) {
	runJS, err := Lookup("noe:run_js", "run_js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runJS, "fresh, owned NOE runtime") || !strings.Contains(runJS, "import them explicitly") || strings.Contains(runJS, "Task recipes are available as details") {
		t.Fatalf("run_js detail lost its shared runtime context or retained the recipe overview: %s", runJS)
	}

	imports, err := Lookup("noe:run_js", "imports")
	if err != nil {
		t.Fatal(err)
	}
	for _, condition := range []string{"configured import-map alias", "local file without an alias", "HTTP imports", "fresh in each run_js call"} {
		if !strings.Contains(imports, condition) {
			t.Errorf("run_js imports detail omits %q", condition)
		}
	}
}

func TestMemberDetailsKeepProviderRequirementsAndEffects(t *testing.T) {
	for _, check := range []struct {
		topic, member string
		conditions    []string
	}{
		{"noe:agent", "run_agent", []string{"root call must provide `provider`, `model`, and `effort`", "may incur cost"}},
		{"noe:agent", "spawn_agent", []string{"root launch requires `provider`, `model`, and `effort`", "may incur cost"}},
		{"noe:decision", "decide", []string{"shared provider-call limit"}},
		{"noe:sockets", "connect", []string{"options.network", `{network: "unix", path, signal?}`, "1–65535"}},
		{"noe:sockets", "listen", []string{"options.network", `{network: "unix", path, signal?}`, "0–65535"}},
		{"noe:image", "generate_image", []string{"external provider request", "requested output path"}},
	} {
		text, err := Lookup(check.topic, check.member)
		if err != nil {
			t.Fatalf("%s.%s: %v", check.topic, check.member, err)
		}
		for _, condition := range check.conditions {
			if !strings.Contains(text, condition) {
				t.Errorf("%s.%s omits shared condition %q", check.topic, check.member, condition)
			}
		}
	}
}

func TestGlobalsSevenDetailOutputReduction(t *testing.T) {
	const topic = "globals"
	members := []string{
		"ReadableStream", "ReadableStream.getReader", "Response", "AbortController",
		"AbortSignal", "setTimeout", "clearTimeout",
	}
	entry := builtin.entries[topic]
	var beforeBytes, afterBytes, beforeChars, afterChars int
	for _, member := range members {
		oldOutput := entry.overview + "\n" + entry.details[member]
		newOutput, err := Lookup(topic, member)
		if err != nil {
			t.Fatal(err)
		}
		if newOutput == oldOutput || len(newOutput) >= len(oldOutput) {
			t.Fatalf("detail %s.%s did not shrink: old=%d new=%d", topic, member, len(oldOutput), len(newOutput))
		}
		beforeBytes += len(oldOutput)
		afterBytes += len(newOutput)
		beforeChars += utf8.RuneCountInString(oldOutput)
		afterChars += utf8.RuneCountInString(newOutput)
	}
	if afterBytes*2 >= beforeBytes {
		t.Fatalf("seven detail lookups should remove repeated overview text: old=%d new=%d", beforeBytes, afterBytes)
	}
	t.Logf("seven globals details: %d characters / %d bytes -> %d characters / %d bytes; saved %d characters / %d bytes", beforeChars, beforeBytes, afterChars, afterBytes, beforeChars-afterChars, beforeBytes-afterBytes)
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
