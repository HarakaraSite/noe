package apidocs

func noeTopics() []topicSpec {
	return []topicSpec{
		{ID: "noe", Kind: "overview", Summary: "NOE is a JavaScript runtime and managed agent execution environment.", File: "noe.md"},
		{ID: "noe:docs", Kind: "module", Summary: "Read the API reference embedded in this NOE binary.", File: "noe-docs.md"},
		{ID: "noe:runtime", Kind: "module", Summary: "Check a module and its static imports without evaluating module bodies.", File: "noe-runtime.md"},
		{ID: "noe:run_js", Kind: "agent tool", Summary: "Execute JavaScript in a fresh, owned NOE runtime; task recipes cover files, commands, imports, HTTP, agents, and failures.", File: "noe-run-js.md"},
		{ID: "noe:agent", Kind: "module", Summary: "Start and manage agents, inspect model choices, and configure task hooks.", File: "noe-agent.md"},
		{ID: "noe:model", Kind: "module", Summary: "List model catalog entries and make one provider request without starting an agent loop.", File: "noe-model.md"},
		{ID: "noe:agent-log", Kind: "module", Summary: "Reconstruct a run from caller-supplied JSONL event data.", File: "noe-agent-log.md"},
		{ID: "noe:decision", Kind: "module", Summary: "Make one structured decision with an explicitly selected OpenRouter model.", File: "noe-decision.md"},
		{ID: "noe:image", Kind: "module", Summary: "Read images for an agent request and generate image files with an explicit model.", File: "noe-image.md"},
		{ID: "noe:auth", Kind: "module", Summary: "Validate and save a completed ChatGPT sign-in result.", File: "noe-auth.md"},
		{ID: "noe:process", Kind: "module", Summary: "Start an owned child process with byte-stream standard I/O.", File: "noe-process.md"},
		{ID: "noe:io", Kind: "module", Summary: "Access host standard I/O streams and pipe Web Streams with active cancellation.", File: "noe-io.md"},
		{ID: "noe:http", Kind: "module", Summary: "Fetch HTTP resources and serve requests with shared Web types and response streams.", File: "noe-http.md"},
		{ID: "noe:fs-handle", Kind: "module", Summary: "Open an existing file for positional byte reads and writes.", File: "noe-fs-handle.md"},
		{ID: "noe:sockets", Kind: "module", Summary: "Open TCP or Unix stream sockets and listeners as byte streams.", File: "noe-sockets.md"},
		{ID: "noe:signals", Kind: "module", Summary: "Subscribe to supported operating-system signals as an async iterator.", File: "noe-signals.md"},
	}
}
