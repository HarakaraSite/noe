package vm

import "errors"

// ModuleLoadError identifies a module request that could not be resolved or
// loaded while a diagnostic link was in progress.
type ModuleLoadError struct {
	Referrer string
	Err      error
}

func (e *ModuleLoadError) Error() string { return e.Err.Error() }
func (e *ModuleLoadError) Unwrap() error { return e.Err }

// ModuleLinkError identifies the module whose exports failed to link.
type ModuleLinkError struct {
	Specifier string
	Err       error
}

func (e *ModuleLinkError) Error() string { return e.Err.Error() }
func (e *ModuleLinkError) Unwrap() error { return e.Err }

// LinkWithPhase links m and reports whether the failure came from resolving a
// dependency or from linking its exports.
func (r *Runtime) LinkWithPhase(m *Module) (phase, source string, err error) {
	previous := r.diagnosticModuleLoads
	r.diagnosticModuleLoads = true
	defer func() { r.diagnosticModuleLoads = previous }()

	err = r.Link(m)
	if err == nil {
		return "", "", nil
	}
	var loadErr *ModuleLoadError
	if errors.As(err, &loadErr) {
		return "resolve", loadErr.Referrer, err
	}
	var linkErr *ModuleLinkError
	if errors.As(err, &linkErr) {
		return "link", linkErr.Specifier, err
	}
	return "link", "", err
}

// LoadAndLinkModule resolves a request against referrer and links its complete
// static dependency graph without evaluating any module body.
func (r *Runtime) LoadAndLinkModule(specifier, referrer string) (m *Module, phase, source string, err error) {
	previous := r.diagnosticModuleLoads
	r.diagnosticModuleLoads = true
	defer func() { r.diagnosticModuleLoads = previous }()

	m, err = r.loadDependency(specifier, referrer)
	if err != nil {
		var loadErr *ModuleLoadError
		if errors.As(err, &loadErr) {
			return nil, "resolve", loadErr.Referrer, err
		}
		return nil, "resolve", referrer, err
	}
	err = r.Link(m)
	if err == nil {
		return m, "", "", nil
	}
	var loadErr *ModuleLoadError
	if errors.As(err, &loadErr) {
		return nil, "resolve", loadErr.Referrer, err
	}
	var linkErr *ModuleLinkError
	if errors.As(err, &linkErr) {
		return nil, "link", linkErr.Specifier, err
	}
	return nil, "link", "", err
}

// ModuleExportNames returns a module's export names without reading any export
// binding. It is for hosts inspecting a linked graph before evaluating it.
func (r *Runtime) ModuleExportNames(m *Module) ([]string, error) {
	return r.namespaceNames(m)
}

// ErrorStackFrames returns frames captured on an Error object without
// formatting or reading its JavaScript stack property.
func (r *Runtime) ErrorStackFrames(value Value) []StackEntry {
	if !value.IsObject() {
		return nil
	}
	trace := r.traceOf(value.Object())
	if trace == nil || trace.done || len(trace.frames) == 0 {
		return nil
	}
	frames := make([]StackEntry, len(trace.frames))
	for i := range trace.frames {
		frame := &trace.frames[i]
		line, column := frame.position()
		frames[i] = StackEntry{
			Function: frame.functionName(),
			Source:   frame.sourceName(),
			Line:     line,
			Column:   column,
		}
	}
	return frames
}

// ErrorObjectStackFrames returns the captured frames of a built-in Error
// object. It does not inspect the JavaScript stack property.
func (r *Runtime) ErrorObjectStackFrames(value Value) []StackEntry {
	if r == nil || r.stack == nil || !value.IsObject() || value.Object().class != ClassError {
		return nil
	}
	return r.ErrorStackFrames(value)
}
