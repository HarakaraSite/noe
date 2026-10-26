package agent

// URLImportFacts describes the selected execution profile without exposing
// its mappings, requests, headers, clients, or other host-owned settings.
func URLImportFacts(support string) string {
	switch support {
	case "enabled":
		return "HTTP/HTTPS ESM URL imports: enabled. Individual URLs remain subject to host policy; arbitrary package compatibility is not implied. NOE's standard URL loader accepts JavaScript MIME types and text/plain, does not transpile TypeScript or discover npm packages, and does not share downloaded source across runtimes. Its synchronous loads pause that runtime's JavaScript jobs; cancellation is driven by the host execution context."
	case "disabled":
		return "HTTP/HTTPS ESM URL imports: disabled by the selected execution profile."
	default:
		return "HTTP/HTTPS ESM URL imports: unknown for the selected custom or legacy module loader. Do not infer availability from the API catalog or from fetch being enabled."
	}
}
