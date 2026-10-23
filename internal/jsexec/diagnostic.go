package jsexec

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// diagnosticError contains only Go data; it remains usable after VM teardown.
// A native Go cause is preserved, but no QuickJS error or value escapes.
type diagnosticError struct {
	message, diagnostic string
	cause               error
	structured          executionresult.Diagnostic
}

func (e *diagnosticError) Error() string { return e.message }
func (e *diagnosticError) Unwrap() error { return e.cause }

type diagnosticData struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
}

// The renderer runs under CallContext. Property details use data descriptors
// so inspecting code/stdout never invokes a user getter. Proxy traps, stack
// rendering and user conversions remain bounded by the execution context.
const diagnosticSource = `(() => {
 const stringify=JSON.stringify,parse=JSON.parse,descriptor=Object.getOwnPropertyDescriptor,create=Object.create,apply=Reflect.apply,hasOwn=Object.prototype.hasOwnProperty;
 return (value,describe,record) => {
   const rendered=parse(describe()),out=create(null);
   out.message=rendered.message;out.stack=rendered.stack;
   if(value!==null&&(typeof value==="object"||typeof value==="function")){
     for(const key of ["code","signal","stdout","stderr"]){
       let entry;try{entry=descriptor(value,key)}catch{continue}
       if(!entry||!apply(hasOwn,entry,["value"]))continue;
       record(key,entry.value);
     }
   }
   return stringify(out);
 };
})()`

func (b *Bindings) snapshotValue(value quickjs.Value, describe func() (string, string), stage string) error {
	fallback := &diagnosticError{message: "JavaScript exception", diagnostic: "JavaScript exception", structured: executionresult.Diagnostic{Stage: stage, Message: "JavaScript exception"}}
	if !b.snapshotter.IsFunction() || b.ctx.Err() != nil {
		return fallback
	}
	details := make(map[string]any)
	record := func(key string, detail quickjs.Value) {
		if key == "" {
			return
		}
		if key == "stdout" || key == "stderr" {
			if content, ok := detail.Bytes(); ok {
				details[key] = executionresult.EncodedBytes{Encoding: "base64", Data: base64.StdEncoding.EncodeToString(append([]byte(nil), content...))}
				return
			}
		}
		switch detail.Kind() {
		case quickjs.KindString:
			details[key] = detail.String()
		case quickjs.KindBool:
			details[key] = detail.Bool()
		case quickjs.KindNumber:
			if f := detail.Float(); !math.IsNaN(f) && !math.IsInf(f, 0) {
				details[key] = f
			}
		}
	}
	encoded, err := b.snapshotter.CallContext(b.ctx, value, func() string {
		message, stack := describe()
		data, _ := json.Marshal(diagnosticData{Message: message, Stack: stack})
		return string(data)
	}, record)
	if err != nil || !encoded.IsString() {
		return fallback
	}
	var data diagnosticData
	if err := json.Unmarshal([]byte(encoded.String()), &data); err != nil {
		return fallback
	}
	diagnostic := data.Message
	if data.Stack != "" {
		diagnostic = data.Stack
	}
	if len(details) > 0 {
		encoded, _ := json.Marshal(details)
		diagnostic += "\nexception details: " + string(encoded)
	}
	return &diagnosticError{
		message: data.Message, diagnostic: diagnostic,
		structured: executionresult.Diagnostic{Stage: stage, Message: data.Message, Stack: data.Stack, Exception: details},
	}
}

func (b *Bindings) snapshotUnhandled(value quickjs.Value) error {
	detached := b.snapshotValue(value, func() (string, string) {
		message := value.String()
		stack := ""
		if value.IsObject() {
			if v, err := value.Get("stack"); err == nil && v.IsString() {
				stack = v.String()
			}
		}
		return message, stack
	}, "execute")
	if b.ctx.Err() != nil || !b.rethrower.IsFunction() {
		return detached
	}
	// Re-throw the already-observed reason through a captured JS function so
	// QuickJS can return its original Go cause as a public *quickjs.Error.
	// This does not run the rejected work or read any additional properties.
	_, thrown := b.rethrower.CallContext(b.ctx, value)
	var jsError *quickjs.Error
	if errors.As(thrown, &jsError) {
		if retained, ok := detached.(*diagnosticError); ok {
			retained.cause = b.retainException(jsError.Unwrap())
		}
	}
	return detached
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
	}, "execute").(*diagnosticError)
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		detached.cause = b.retainException(wrapped.Unwrap())
	}
	return detached
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
