package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestContextHistoryWholeBlocksProtectedEntriesAndObjectIdentity(t *testing.T) {
	source := `import {TaskHistory} from "noe:agent/history";
const h = new TaskHistory({role:"system",content:"system"},{role:"user",content:"task"});
const old = {role:"assistant",content:"old"};
h.completeBlock(old);
h.appendSteer({commandId:"s1",instruction:"keep steer"});
h.appendSteer({commandId:"s1",instruction:"duplicate"});
const recent = {role:"assistant",reasoning_details:[{signature:"signed"}],nativeResponseItems:[{opaque:1}],tool_calls:[{id:"a"},{id:"b"}]};
h.completeBlock(recent,[{stdout:"first"},{stdout:"second"}]);
const before=h.layout();
let rejected=0;
for (const plan of [{revision:before.revision-1,removeIds:[before.entries[2].id]}, {revision:before.revision,removeIds:[before.entries[0].id]}, {revision:before.revision,removeIds:[before.entries[2].id,before.entries[4].id]}]) {
 try { h.apply(plan); } catch (_) { rejected++; }
}
if(rejected!==3) throw Error("invalid deletion accepted");
const staged=h.stage({revision:before.revision,removeIds:[before.entries[2].id]});
if(!h.messages().includes(old)) throw Error("staging deleted uncommitted history");
h.commit(staged);
const m=h.messages();
if(m.length!==6 || m[0].content!=="system" || m[1].content!=="task" || m[2].content!=="keep steer" || m[3]!==recent || m[4].tool_call_id!=="a" || m[5].tool_call_id!=="b") throw Error("history corrupted");
if(h.entries.some(e=>e.messages.includes(old))) throw Error("deleted object retained");
let incompleteRejected=false;
try {h.completeBlock(recent,[{}]);} catch(e) {if(!e.message.includes("incomplete"))throw e;incompleteRejected=true;}
if(!incompleteRejected)throw Error("partial block accepted");
console.log("history ok");`
	var out strings.Builder
	code, err := Execute(context.Background(), Source{Name: "history-test.js", Code: source}, Options{Stdout: &out})
	if code != 0 || err != nil || out.String() != "history ok\n" {
		t.Fatalf("code=%d err=%v output=%s", code, err, out.String())
	}
}
