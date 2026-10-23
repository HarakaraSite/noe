// Package logbinding installs synchronous data-only event analysis.
package logbinding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	"forge.harakara.site/littleisland/noe/v2/internal/loganalysis"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const moduleSource = `import {analyze} from "noe:agent-log/internal";
export function analyze_log(input,options={}){
 if(typeof input!=="string"&&!(input instanceof Uint8Array))throw new TypeError("log input must be a string or Uint8Array");
 if(options===null||typeof options!=="object"||Array.isArray(options))throw new TypeError("log options must be an object");
 for(const key of Object.keys(options)){if(!["runId","cursor","includeThinking","includeOutput"].includes(key))throw new TypeError("unknown log option: "+key)}
 if(typeof options.runId!=="string"||!options.runId)throw new TypeError("runId is required");
 for(const key of ["includeThinking","includeOutput"]){if(options[key]!==undefined&&typeof options[key]!=="boolean")throw new TypeError(key+" must be a boolean")}
 if(options.cursor!==undefined){const c=options.cursor;if(c===null||typeof c!=="object"||Array.isArray(c)||typeof c.executionId!=="string"||!Number.isSafeInteger(c.seq)||c.seq<0)throw new TypeError("invalid log cursor")}
 return JSON.parse(analyze(input,JSON.stringify(options)));
}`

type Binding struct {
	ctx      context.Context
	cancel   context.CancelFunc
	stopView func() bool
	once     sync.Once
}

func Bind(ctx context.Context, view *stdlib.Bindings) (*Binding, error) {
	if ctx == nil || view == nil || view.Context() == nil || view.Runtime() == nil || view.Loop() == nil {
		return nil, errors.New("log: live bindings and context are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := view.Context().Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	b := &Binding{ctx: ctx, cancel: cancel}
	b.stopView = context.AfterFunc(view.Context(), cancel)
	if err := view.DefineModule("noe:agent-log", moduleSource); err != nil {
		b.Close()
		return nil, err
	}
	if err := view.Runtime().SetModule("noe:agent-log/internal", map[string]any{"analyze": b.analyze}); err != nil {
		b.Close()
		return nil, err
	}
	view.Runtime().OnClose(b.Close)
	return b, nil
}
func (b *Binding) Close() {
	if b != nil {
		b.once.Do(func() { b.cancel(); b.stopView() })
	}
}
func (b *Binding) analyze(input quickjs.Value, options string) (string, error) {
	if err := b.ctx.Err(); err != nil {
		return "", err
	}
	var data []byte
	if input.IsString() {
		data = []byte(input.String())
	} else {
		value, ok := input.Bytes()
		if !ok {
			return "", errors.New("log input must be a string or Uint8Array")
		}
		data = append([]byte(nil), value...)
	}
	decoder := json.NewDecoder(bytes.NewBufferString(options))
	decoder.DisallowUnknownFields()
	var query loganalysis.Query
	if err := decoder.Decode(&query); err != nil {
		return "", err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", errors.New("invalid log options")
	}
	snapshot, err := loganalysis.Analyze(b.ctx, bytes.NewReader(data), query)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(snapshot)
	return string(encoded), err
}
