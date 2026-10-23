package apidocs

import "strings"

// Signature reads explicitly authored metadata from the member's reference.
// A missing signature means undocumented, rather than an unavailable export.
func Signature(topic, member string) string {
	text, err := Lookup(topic, member)
	if err != nil || member == "" {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		if value, ok := strings.CutPrefix(line, "Signature: "); ok {
			return strings.Trim(strings.TrimSpace(value), "`")
		}
	}
	return ""
}
