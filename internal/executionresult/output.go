package executionresult

// OutputFile describes the bytes saved for one run_js stream or JSON value.
type OutputFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}
