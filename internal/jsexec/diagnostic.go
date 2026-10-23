package jsexec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
)

// diagnosticError contains only Go data; it remains usable after VM teardown.
// A native Go cause is preserved, but no QuickJS error or value escapes.
type diagnosticError struct {
	message, diagnostic string
	cause               error
	info                DiagnosticInfo
}

func (e *diagnosticError) Error() string { return e.message }
func (e *diagnosticError) Unwrap() error { return e.cause }

type diagnosticData struct {
	Message string         `json:"message"`
	Stack   string         `json:"stack"`
	Details map[string]any `json:"details"`
}

// StackFrame is a detached JavaScript stack entry. It contains only source
// facts captured by the engine, never a JavaScript value.
type StackFrame struct {
	Function string `json:"function,omitempty"`
	Source   string `json:"source,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
}

// DiagnosticInfo describes one execution or module-check failure using facts
// detached from its VM.
type DiagnosticInfo struct {
	Code      string         `json:"code"`
	Phase     string         `json:"phase,omitempty"`
	Message   string         `json:"message"`
	Source    string         `json:"source,omitempty"`
	Line      int            `json:"line,omitempty"`
	Column    int            `json:"column,omitempty"`
	Stack     []StackFrame   `json:"stack,omitempty"`
	Exception map[string]any `json:"exception,omitempty"`
}

const (
	codeJSSyntax    = "JS_SYNTAX"
	codeJSModule    = "JS_MODULE"
	codeJSException = "JS_EXCEPTION"
	codeJSResult    = "JS_RESULT"
	codeJSCancelled = "JS_CANCELLED"
	codeJSTimeout   = "JS_TIMEOUT"
	codeJSHost      = "JS_HOST"
)

// The renderer runs under CallContext. Property details use data descriptors
// so inspecting code/stdout never invokes a user getter. Proxy traps, stack
// rendering and user conversions remain bounded by the execution context.
const diagnosticSource = `(() => {
 const stringify=JSON.stringify,parse=JSON.parse,descriptor=Object.getOwnPropertyDescriptor,create=Object.create,finite=Number.isFinite;
 return (value,describe) => {
   const rendered=parse(describe()),out=create(null),details=create(null);
   out.message=rendered.message;out.stack=rendered.stack;
   if(value!==null&&(typeof value==="object"||typeof value==="function")){
     for(const key of ["code","signal","stdout","stderr"]){
       let entry;try{entry=descriptor(value,key)}catch{continue}
       if(!entry)continue;
       const data=descriptor(entry,"value");if(!data)continue;
       const v=data.value,type=typeof v;
       if(type==="string"||type==="boolean"||(type==="number"&&finite(v)))details[key]=v;
     }
   }
   out.details=details;return stringify(out);
 };
})()`

func (b *Bindings) snapshotValue(value quickjs.Value, describe func() (string, string), original error) error {
	var valueFrames []quickjs.StackFrame
	if original == nil {
		// snapshotUnhandled has no *quickjs.Error wrapper. Preserve the engine's
		// frames before stack rendering can consume the Error's lazy trace.
		valueFrames = value.ErrorFrames()
	}
	fallbackInfo := diagnosticInfo(original, "JavaScript exception")
	fillErrorFrames(&fallbackInfo, original)
	if original == nil {
		fillStackFrames(&fallbackInfo, valueFrames)
	}
	fallback := &diagnosticError{message: "JavaScript exception", diagnostic: "JavaScript exception", info: fallbackInfo}
	if !b.snapshotter.IsFunction() || b.ctx.Err() != nil {
		return fallback
	}
	encoded, err := b.snapshotter.CallContext(b.ctx, value, func() string {
		message, stack := describe()
		data, _ := json.Marshal(diagnosticData{Message: message, Stack: stack})
		return string(data)
	})
	if err != nil || !encoded.IsString() {
		return fallback
	}
	var data diagnosticData
	if err := json.Unmarshal([]byte(encoded.String()), &data); err != nil {
		return fallback
	}
	info := diagnosticInfo(original, data.Message)
	if data.Message != "" {
		info.Message = data.Message
	}
	if details := cloneDetails(data.Details); len(details) > 0 {
		info.Exception = details
	}
	fillErrorFrames(&info, original)
	if original == nil {
		fillStackFrames(&info, valueFrames)
	}
	diagnostic := data.Message
	if data.Stack != "" {
		diagnostic = data.Stack
	}
	if len(data.Details) > 0 {
		encoded, _ := json.Marshal(data.Details)
		diagnostic += "\nexception details: " + string(encoded)
	}
	return &diagnosticError{message: data.Message, diagnostic: diagnostic, info: info}
}

func fillErrorFrames(info *DiagnosticInfo, err error) {
	var jsErr *quickjs.Error
	if !errors.As(err, &jsErr) {
		return
	}
	fillStackFrames(info, jsErr.Frames())
}

func fillStackFrames(info *DiagnosticInfo, frames []quickjs.StackFrame) {
	for _, frame := range frames {
		info.Stack = append(info.Stack, StackFrame{
			Function: frame.Function,
			Source:   frame.Source,
			Line:     frame.Line,
			Column:   frame.Column,
		})
	}
	if info.Source == "" && len(info.Stack) > 0 {
		info.Source = info.Stack[0].Source
		info.Line = info.Stack[0].Line
		info.Column = info.Stack[0].Column
	}
}

func (b *Bindings) snapshotUnhandled(value quickjs.Value) error {
	return b.snapshotValue(value, func() (string, string) {
		message := value.String()
		stack := ""
		if value.IsObject() {
			if v, err := value.Get("stack"); err == nil && v.IsString() {
				stack = v.String()
			}
		}
		return message, stack
	}, nil)
}

func (b *Bindings) retainException(err error) error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := append([]error(nil), joined.Unwrap()...)
		for i, cause := range causes {
			causes[i] = b.retainException(cause)
		}
		return errors.Join(causes...)
	}
	var jsError *quickjs.Error
	if !errors.As(err, &jsError) {
		return err
	}
	detached := b.snapshotValue(jsError.Value(), func() (string, string) {
		// These calls may run user JS. CallContext above is still active here.
		message := err.Error()
		stack := jsError.Stack()
		if err != jsError {
			stack = strings.Replace(message, jsError.Error(), stack, 1)
		}
		return message, stack
	}, err).(*diagnosticError)
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		detached.cause = b.retainException(wrapped.Unwrap())
	}
	return detached
}

func cloneDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	clone := make(map[string]any, len(details))
	for key, value := range details {
		clone[key] = value
	}
	return clone
}

func diagnosticInfo(err error, fallbackMessage string) DiagnosticInfo {
	info := DiagnosticInfo{Code: codeJSHost, Message: fallbackMessage}
	if err == nil {
		info.Code = codeJSException
		return info
	}
	var retained *diagnosticError
	if errors.As(err, &retained) {
		info = retained.info
		if info.Message == "" {
			info.Message = fallbackMessage
		}
		return info
	}
	contextFailure := false
	if errors.Is(err, context.Canceled) {
		info.Code = codeJSCancelled
		contextFailure = true
	} else if errors.Is(err, context.DeadlineExceeded) {
		info.Code = codeJSTimeout
		contextFailure = true
	}
	var moduleErr *quickjs.ModuleError
	if errors.As(err, &moduleErr) {
		info.Phase = string(moduleErr.Phase)
		info.Source = moduleErr.Source
		if !contextFailure {
			switch moduleErr.Phase {
			case quickjs.ModulePhaseParse, quickjs.ModulePhaseCompile:
				info.Code = codeJSSyntax
			case quickjs.ModulePhaseResolve, quickjs.ModulePhaseLink:
				info.Code = codeJSModule
			}
		}
	}
	var syntaxErr *quickjs.SyntaxError
	if errors.As(err, &syntaxErr) {
		file, line, column := syntaxErr.Position()
		if info.Source == "" {
			info.Source = file
		}
		info.Line, info.Column = line, column
		if !contextFailure {
			info.Code = codeJSSyntax
		}
	}
	var jsErr *quickjs.Error
	if errors.As(err, &jsErr) && !contextFailure {
		if jsErr.Unwrap() == nil && info.Code != codeJSModule && info.Code != codeJSSyntax {
			info.Code = codeJSException
		} else if jsErr.Unwrap() != nil && info.Phase != string(quickjs.ModulePhaseResolve) && info.Phase != string(quickjs.ModulePhaseLink) {
			info.Code = codeJSHost
		}
	}
	if info.Message == "" {
		// Errors reaching this path have already been detached by retainException,
		// or are engine-owned Go errors with no live JavaScript conversion.
		info.Message = err.Error()
	}
	return info
}

func diagnosticInfos(err error) []DiagnosticInfo {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var infos []DiagnosticInfo
		for _, cause := range joined.Unwrap() {
			infos = append(infos, diagnosticInfos(cause)...)
		}
		return infos
	}
	return []DiagnosticInfo{diagnosticInfo(err, "")}
}

func fillDiagnosticPhase(infos []DiagnosticInfo, phase string) []DiagnosticInfo {
	for i := range infos {
		if infos[i].Phase == "" {
			infos[i].Phase = phase
		}
	}
	return infos
}

// Setup failures before a binding exists cannot inspect JS safely. This
// fallback strips live JS values and preserves native Go causes without VM IO.
func retainException(err error) error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := append([]error(nil), joined.Unwrap()...)
		for i, cause := range causes {
			causes[i] = retainException(cause)
		}
		return errors.Join(causes...)
	}
	var jsError *quickjs.Error
	if errors.As(err, &jsError) {
		var cause error
		if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			cause = retainException(wrapped.Unwrap())
		}
		return &diagnosticError{message: "JavaScript exception", diagnostic: "JavaScript exception", cause: cause}
	}
	return err
}

// Diagnostic returns detached JavaScript stacks and available command details.
func Diagnostic(err error) string {
	if err == nil {
		return ""
	}
	if retained, ok := err.(*diagnosticError); ok {
		return retained.diagnostic
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var parts []string
		for _, cause := range joined.Unwrap() {
			parts = append(parts, Diagnostic(cause))
		}
		return strings.Join(parts, "\n")
	}
	// Foreign, uncaptured JS errors must not execute user conversions here.
	var jsError *quickjs.Error
	if errors.As(err, &jsError) {
		return "JavaScript exception"
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if cause := wrapped.Unwrap(); cause != nil {
			return strings.Replace(err.Error(), cause.Error(), Diagnostic(cause), 1)
		}
	}
	return err.Error()
}
