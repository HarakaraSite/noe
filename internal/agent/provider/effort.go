package provider

// NormalizeEffort accepts NOE's auto default and OpenRouter's effort levels.
// Empty values come from internal callers that omit this optional setting.
func NormalizeEffort(value string) (string, bool) {
	switch value {
	case "", "auto":
		return "auto", true
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return value, true
	default:
		return "", false
	}
}
