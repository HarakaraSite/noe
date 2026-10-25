package agentbridge

import (
	"errors"
	"strings"
	"testing"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestHookRunnerDoesNotRecoverScopeOrBrokerFailures(t *testing.T) {
	for _, test := range []struct {
		name, begin, response, end string
		beginErr, endErr           bool
		want                       string
		recovered                  bool
	}{
		{name: "callback", recovered: true},
		{name: "scope begin", beginErr: true, want: "begin infrastructure"},
		{name: "scope shape", begin: `{"scopeId":"","defaults":{}}`, want: "invalid hook scope"},
		{name: "scope end wins over callback", endErr: true, want: "end infrastructure"},
		{name: "scope end shape", end: `{"closed":false}`, want: "invalid hook scope close"},
		{name: "RPC JSON caught by handler", response: "not JSON", want: "SyntaxError"},
		{name: "RPC shape caught by handler", response: `{"context":null}`, want: "invalid hook response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rt := quickjs.New()
			defer rt.Close()
			rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
				source, ok := moduleSource(specifier)
				if !ok {
					return "", "", errors.New("unexpected module")
				}
				return source, specifier, nil
			})
			err := rt.SetModule("noe:agent/internal", map[string]any{
				"beginHookScope": func(string) (string, error) {
					if test.beginErr {
						return "", errors.New("begin infrastructure")
					}
					if test.begin != "" {
						return test.begin, nil
					}
					return `{"scopeId":"scope","defaults":{},"event":{"runId":"run","modelStepId":"step"}}`, nil
				},
				"requestHook": func(string, string, string) (string, error) {
					if test.response != "" {
						return test.response, nil
					}
					return `{"context":{}}`, nil
				},
				"endHookScope": func(string, string, string, string) (string, error) {
					if test.endErr {
						return "", errors.New("end infrastructure")
					}
					if test.end != "" {
						return test.end, nil
					}
					return `{"closed":true}`, nil
				},
				"resolveHookModule": func(module string, _ int) string { return module },
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = rt.EvalModule("[hook boundary test]", `
import {callHooks} from "noe:agent/hooks";
globalThis.invoked=0;
const loaded=[{module:"fixture",index:0,handlers:{
 beforeModelRequest:async(_,api)=>{
  globalThis.invoked++;
  try {await api.context.get();}catch{}
  throw Error("callback failure");
 }
}},{module:"later",index:1,handlers:{beforeModelRequest:()=>{globalThis.invoked++;}}}];
try {
 await callHooks(loaded,"beforeModelRequest",{runId:"run",modelStepId:"step"});
 globalThis.result="recovered";
} catch(error) {globalThis.result=String(error);}
`)
			if err != nil {
				t.Fatal(err)
			}
			value, err := rt.Get("result")
			if err != nil {
				t.Fatal(err)
			}
			result := value.String()
			invoked, _ := rt.Get("invoked")
			if test.recovered {
				if result != "recovered" || invoked.Int() != 2 {
					t.Fatalf("result=%s calls=%d", result, invoked.Int())
				}
			} else if !strings.Contains(result, test.want) || result == "recovered" || strings.Contains(result, "callback failure") || invoked.Int() > 1 {
				t.Fatalf("infrastructure failure hidden: result=%s calls=%d want=%s", result, invoked.Int(), test.want)
			}
		})
	}
}
