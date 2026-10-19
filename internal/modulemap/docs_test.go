package modulemap

import (
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/apidocs"
)

func TestDocsCoverPinnedStdlibModuleRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, topic := range apidocs.Topics() {
		if !strings.HasPrefix(topic.ID, "node:") {
			continue
		}
		bare := strings.TrimPrefix(topic.ID, "node:")
		if _, ok := standardLibraryBareModules[bare]; !ok {
			t.Errorf("docs lists unregistered module %s", topic.ID)
		}
		seen[bare] = true
	}
	for bare := range standardLibraryBareModules {
		if !seen[bare] {
			t.Errorf("registered module missing from docs: %s", bare)
		}
	}
}
