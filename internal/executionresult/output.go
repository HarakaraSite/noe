package executionresult

// OutputFile describes the raw bytes saved for one run_js stream.
type OutputFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}
