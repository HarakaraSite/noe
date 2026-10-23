package agent

import (
	"encoding/json"
	"fmt"
	quickjs "github.com/go-quickjs/go-quickjs"
	"testing"
)

func TestToolReportingReusesPassThroughHookJSON(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	var reports []map[string]json.RawMessage
	err := rt.SetModule("noe:agent/internal", map[string]any{
		"request": func(method, payload string) (string, error) {
			if method != "tool_result" {
				return "", fmt.Errorf("unexpected request %s", method)
			}
			var report map[string]json.RawMessage
			if err := json.Unmarshal([]byte(payload), &report); err != nil {
				return "", err
			}
			reports = append(reports, report)
			return `{"recorded":true}`, nil
		},
		"defaults": func() string { return `{}` }, "instructions": func() string { return "" },
		"beginHookScope": func(payload string) string {
			return `{"scopeId":"fixture-scope","defaults":{},"event":` + payload + `}`
		},
		"endHookScope":      func(string, string, string) string { return `{}` },
		"requestHook":       func(string, string, string) string { return `{}` },
		"resolveHookModule": func(name string, index int) string { return name },
	})
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{"noe:agent/task": TaskModuleSource, "noe:agent/hooks": HooksModuleSource, "noe:agent/api": APIModuleSource, "noe:agent/history": HistoryModuleSource}
	rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		source, ok := sources[specifier]
		if !ok {
			return "", "", fmt.Errorf("unknown fixture module %s", specifier)
		}
		return source, specifier, nil
	})
	_, err = rt.EvalModuleContext(t.Context(), "[report fixture]", `
import {executeToolWithHooks} from "noe:agent/task";
const event={runId:"run",modelStepId:"step"};
const call=id=>({id,type:"function",function:{name:"run_js",arguments:JSON.stringify({code:"original"})}});
const raw={stdout:"",stderr:"",exitCode:0,reason:"completed",data:{a:[1,null],b:true}};
const unchanged=[{module:"pass-through",index:0,handlers:{
 beforeTool(e){return {arguments:{code:e.arguments.code}};},
 afterTool(e){return {result:{data:{b:e.result.data.b,a:e.result.data.a},reason:e.result.reason,exitCode:e.result.exitCode,stderr:e.result.stderr,stdout:e.result.stdout}};}
}}];
await executeToolWithHooks(unchanged,event,call("same"),async effective=>{if(effective.function.arguments!==JSON.stringify({code:"original"}))throw Error("unchanged source rewritten");return raw;});
const changed=[{module:"change",index:0,handlers:{
 beforeTool(e){return {arguments:{code:e.arguments.code+" changed"}};},
 afterTool(e){return {result:{...e.result,data:{...e.result.data,a:[2,null]}}};}
}}];
await executeToolWithHooks(changed,event,call("changed"),async effective=>{if(JSON.parse(effective.function.arguments).code!=="original changed")throw Error("changed source lost");return raw;});`)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 {
		t.Fatalf("reports=%d", len(reports))
	}
	same := reports[0]
	if string(same["argumentsUnchanged"]) != "true" || string(same["resultUnchanged"]) != "true" || len(same["arguments"]) != 0 || len(same["result"]) != 0 {
		t.Fatalf("pass-through JSON sent twice: %s", mustReportJSON(t, same))
	}
	changed := reports[1]
	if len(changed["argumentsUnchanged"]) != 0 || len(changed["resultUnchanged"]) != 0 || len(changed["arguments"]) == 0 || len(changed["result"]) == 0 {
		t.Fatalf("actual replacement omitted: %s", mustReportJSON(t, changed))
	}
}

func mustReportJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
