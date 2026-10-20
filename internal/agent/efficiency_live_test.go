//go:build agent_efficiency_live

package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

// Explicit opt-in: real ChatGPT-plan inference, never selected by normal tests.
// The transport records hashes and cache/session facts, never credentials or
// request/response bodies. events.jsonl contains the ordinary NOE observations.
func TestAgentEfficiencyLive(t *testing.T) {
	if os.Getenv("NOE_AGENT_EFFICIENCY_LIVE") != "1" {
		t.Skip("set NOE_AGENT_EFFICIENCY_LIVE=1 to use the ChatGPT plan")
	}
	artifactRoot := os.Getenv("NOE_AGENT_EFFICIENCY_LIVE_DIR")
	if !filepath.IsAbs(artifactRoot) {
		t.Fatal("NOE_AGENT_EFFICIENCY_LIVE_DIR must be an absolute artifact directory")
	}
	mode := os.Getenv("NOE_AGENT_EFFICIENCY_POLICY_MODE")
	if mode == "" {
		mode = "external"
	}
	var policy []byte
	switch mode {
	case "external":
		_, source, _, _ := runtime.Caller(0)
		var err error
		policy, err = os.ReadFile(filepath.Join(filepath.Dir(source), "../../.noe/instructions/delegation.md"))
		if err != nil {
			t.Fatal(err)
		}
	case "builtin":
	default:
		t.Fatal("policy mode must be external or builtin")
	}
	for _, name := range []string{"small_queue", "implementation_and_review"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(artifactRoot, name)
			if err := os.MkdirAll(artifactRoot, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatalf("use a fresh artifact directory: %v", err)
			}
			if mode == "external" {
				writeInstructionTestFile(t, filepath.Join(dir, ".noe/instructions/delegation.md"), string(policy))
			}
			runEfficiencyTrial(t, dir, name, mode)
		})
	}
}

type efficiencyWire struct {
	SessionID       string `json:"sessionId"`
	Model           string `json:"model"`
	InputItems      int    `json:"inputItems"`
	FixedHash       string `json:"fixedHash"`
	ToolsHash       string `json:"toolsHash"`
	PreviousItems   int    `json:"previousItems"`
	CommonItems     int    `json:"commonItems"`
	PrefixPreserved bool   `json:"prefixPreserved"`
	StatusRole      string `json:"statusRole"`
	StatusCount     int    `json:"statusCount"`
	PolicyPresent   bool   `json:"policyPresent"`
	HTTPStatus      int    `json:"httpStatus"`
}

type efficiencyTransport struct {
	mu       sync.Mutex
	wire     []*efficiencyWire
	previous map[string][]string
}

func efficiencyHash(value json.RawMessage) string {
	var decoded any
	_ = json.Unmarshal(value, &decoded)
	canonical, _ := json.Marshal(decoded)
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:])
}

func (c *efficiencyTransport) Do(req *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(req.URL.Path, "/responses") {
		return http.DefaultClient.Do(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	var request struct {
		Model string            `json:"model"`
		Key   string            `json:"prompt_cache_key"`
		Input []json.RawMessage `json:"input"`
		Tools json.RawMessage   `json:"tools"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	record := &efficiencyWire{SessionID: request.Key, Model: request.Model, InputItems: len(request.Input), ToolsHash: efficiencyHash(request.Tools)}
	hashes := make([]string, len(request.Input))
	var fixed []json.RawMessage
	for index, item := range request.Input {
		hashes[index] = efficiencyHash(item)
		var message struct{ Role, Content string }
		if json.Unmarshal(item, &message) == nil {
			if message.Role == "system" || message.Role == "developer" {
				fixed = append(fixed, item)
				record.PolicyPresent = record.PolicyPresent || strings.Contains(message.Content, "子agentへの委譲は")
			}
			if strings.HasPrefix(message.Content, "NOE_CONTEXT_STATUS\n") {
				record.StatusCount++
				if index == len(request.Input)-1 {
					record.StatusRole = message.Role
				}
			}
		}
	}
	fixedJSON, _ := json.Marshal(fixed)
	record.FixedHash = efficiencyHash(fixedJSON)
	c.mu.Lock()
	previous := c.previous[request.Key]
	record.PreviousItems = len(previous)
	for record.CommonItems < len(previous) && record.CommonItems < len(hashes) && previous[record.CommonItems] == hashes[record.CommonItems] {
		record.CommonItems++
	}
	record.PrefixPreserved = record.CommonItems == len(previous)
	c.previous[request.Key] = hashes
	c.wire = append(c.wire, record)
	c.mu.Unlock()
	response, err := http.DefaultClient.Do(req)
	if response != nil {
		c.mu.Lock()
		record.HTTPStatus = response.StatusCode
		c.mu.Unlock()
	}
	return response, err
}

func runEfficiencyTrial(t *testing.T, dir, name, mode string) {
	t.Helper()
	transport := &efficiencyTransport{previous: make(map[string][]string)}
	var observed []events.Event
	var eventMu sync.Mutex
	defer func() {
		eventMu.Lock()
		defer eventMu.Unlock()
		file, err := os.Create(filepath.Join(dir, "events.jsonl"))
		if err != nil {
			t.Error(err)
			return
		}
		writer := events.New(file, "efficiency-live")
		for _, event := range observed {
			writer.Emit(event)
		}
		if err := writer.Err(); err != nil {
			t.Error(err)
		}
		_ = file.Close()
		transport.mu.Lock()
		defer transport.mu.Unlock()
		report := map[string]any{"scenario": name, "policyMode": mode, "provider": "openai-chatgpt", "model": "gpt-6.1-sol", "effort": "high", "passed": !t.Failed(), "wire": transport.wire}
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "wire.json"), append(data, '\n'), 0600); err != nil {
			t.Error(err)
		}
	}()
	h := NewHost(Options{ProviderOptions: provider.Options{HTTPClient: transport}, Executable: lifecycleBinary(t), Cwd: dir, Observe: func(event events.Event) {
		// Capture values now: event payloads may otherwise refer to live state.
		encoded, _ := json.Marshal(event)
		var copy events.Event
		_ = json.Unmarshal(encoded, &copy)
		eventMu.Lock()
		observed = append(observed, copy)
		eventMu.Unlock()
	}})
	defer h.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	op := h.NewOperation(ctx)
	defer op.Close()
	limit := int64(24)
	if name == "small_queue" {
		limit = 10
	}
	starts := int64(4)
	config := taskConfig{Config: Config{Provider: "openai-chatgpt", Model: "gpt-6.1-sol", Effort: "high", Name: name, MaxTokens: 8192,
		contextPolicyPresent: true, ExecutionLimits: &ExecutionLimits{ProviderCalls: &limit, AgentStarts: &starts}}, MaxSteps: 10}
	var tasks []string
	if name == "small_queue" {
		writeInstructionTestFile(t, filepath.Join(dir, "numbers.json"), `[5,1,5,-2,0,11]`)
		tasks = []string{
			"numbers.jsonを実際に読み、重複を除き昇順にした配列をsorted.jsonへ保存してください。必要な検査を行い、結果を簡潔に返してください。変更権限はこの一時workspace内です。",
			"続いてsorted.jsonを読み、count・sum・min・maxを持つsummary.jsonを保存してください。既存のsorted.jsonは変えず、元のnumbers.jsonに対して結果が正しいことを確認してください。",
			"最後に、保存済みのsorted.jsonとsummary.jsonの整合性を実際に検査してください。正しければファイルを変更せず完了してください。新しい要件や追加調査は不要です。",
		}
	} else {
		writeInstructionTestFile(t, filepath.Join(dir, "spec.md"), efficiencyLedgerSpec)
		tasks = []string{"spec.mdに従ってledger.jsとcheck.jsを作成してください。一つの子に実装と検査を分担し、別の子に実装者から独立したreviewを依頼してください。reviewは通常ケースの確認後、追加の負値・空配列ケースを同じ担当へ再確認させてください。担当ごとにまとまった範囲を渡し、結果を受けて親が最終検査・報告を行ってください。子の再確認には元の担当を再利用できる起動方法を選んでください。成果物の権限はこの一時workspace内です。履歴や全ログの転記は不要です。"}
	}
	spawned, err := h.spawn(op, spawnRequest{Config: config, Instruction: &tasks[0], KeepAlive: true})
	if err != nil {
		t.Fatal(err)
	}
	for index, task := range tasks {
		commandID := spawned.CommandID
		if index > 0 {
			var receipt commandReceipt
			_ = json.Unmarshal([]byte(lifecycleCall(t, op, "queue_agent", queueRequest{AgentID: spawned.AgentID, Instruction: task})), &receipt)
			commandID = receipt.CommandID
		}
		outcome := taskWaitResult(t, op, spawned.AgentID, commandID)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("answer-%d.txt", index+1)), []byte(outcome.Answer), 0600); err != nil {
			t.Fatal(err)
		}
		if outcome.State != "completed" {
			t.Fatalf("task %d ended %s: %s", index+1, outcome.State, outcome.Error)
		}
	}
	lifecycleCall(t, op, "interrupt_agent", interruptRequest{AgentID: spawned.AgentID})
	lifecycleSnapshot(t, op, spawned.AgentID, true)
	if name == "small_queue" {
		for file, want := range map[string]string{"sorted.json": `[-2,0,1,5,11]`, "summary.json": `{"count":5,"sum":15,"min":-2,"max":11}`} {
			got, err := os.ReadFile(filepath.Join(dir, file))
			if err != nil {
				t.Fatal(err)
			}
			if efficiencyHash(got) != efficiencyHash(json.RawMessage(want)) {
				t.Fatalf("wrong %s: %s", file, got)
			}
		}
	} else {
		verifyEfficiencyLedger(t, dir)
	}
	eventMu.Lock()
	runs := make(map[string]string)
	instances := make(map[string]string)
	childRuns := make(map[string]int)
	for _, event := range observed {
		switch event["type"] {
		case "instance.started":
			instances[event["agentId"].(string)], _ = event["parentRunId"].(string)
		case "agent.started":
			runs[event["runId"].(string)] = event["agentId"].(string)
			if event["parentRunId"] != "" {
				childRuns[event["agentId"].(string)]++
			}
		}
	}
	eventMu.Unlock()
	children, reused := 0, false
	for id, parentRun := range instances {
		if parentRun != "" {
			children++
			if runs[parentRun] != spawned.AgentID {
				t.Errorf("grandchild started: %s", id)
			}
		}
		if childRuns[id] > 1 {
			reused = true
		}
	}
	if name == "small_queue" && len(runs) != 3 {
		t.Errorf("root task runs=%d, want three", len(runs))
	}
	if name == "small_queue" && children != 0 {
		t.Errorf("small work started %d children", children)
	}
	if name != "small_queue" && (children != 2 || !reused) {
		t.Errorf("expected two children and reuse: children=%d reused=%v", children, reused)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	first := make(map[string]*efficiencyWire)
	for _, wire := range transport.wire {
		if wire.SessionID == "" || !wire.PrefixPreserved || wire.StatusRole != "user" || !wire.PolicyPresent || wire.HTTPStatus != 200 {
			t.Errorf("wire invariants failed: %+v", wire)
		}
		if previous := first[wire.SessionID]; previous != nil {
			if previous.FixedHash != wire.FixedHash || previous.ToolsHash != wire.ToolsHash {
				t.Error("fixed instruction or tools changed within an instance")
			}
		} else {
			first[wire.SessionID] = wire
		}
	}
	if len(first) != len(instances) {
		t.Errorf("session IDs=%d, instances=%d", len(first), len(instances))
	}
}

const efficiencyLedgerSpec = `Create two ES modules for the NOE runtime, without third-party libraries.
ledger.js exports summarize(rows). Each row is {id, category, amount}.
id and category must be nonempty strings; amount must be a finite integer.
Duplicate ids or malformed rows throw Error. Empty input is valid.
Return {count,total,groups}; groups is an array of {category,count,total}, sorted
by category using JavaScript string comparison, not locale-dependent sorting.
Do not mutate rows or row objects. Negative and zero amounts are valid.
check.js imports summarize and runs meaningful assertions for ordinary groups,
invalid input, duplicate IDs, sorting, and input preservation. Use plain JavaScript
assertions; execute with NOE. Print one concise success line. Avoid global state.
The parent must independently verify the result. Review is read-only.
`

func verifyEfficiencyLedger(t *testing.T, dir string) {
	t.Helper()
	// Independent acceptance input, including category ordering and negatives.
	source := `import {summarize} from "./ledger.js";
const rows=[{id:"a",category:"z",amount:-3},{id:"b",category:"A",amount:0},{id:"c",category:"z",amount:8}];
const before=JSON.stringify(rows);
const got=summarize(rows);
if(got.count!==3 || got.total!==5 || got.groups.length!==2 || got.groups[0].category!=="A" || got.groups[0].count!==1 || got.groups[0].total!==0 || got.groups[1].category!=="z" || got.groups[1].count!==2 || got.groups[1].total!==5)throw Error(JSON.stringify(got));
if(JSON.stringify(rows)!==before)throw Error("mutated input");
const empty=summarize([]);if(empty.count!==0 || empty.total!==0 || empty.groups.length!==0)throw Error("empty input");
for(const invalid of [[{id:"x",category:"y",amount:NaN}],[{id:"a",category:"x",amount:1},{id:"a",category:"y",amount:2}]]){
let rejected=false;try{summarize(invalid);}catch(_){rejected=true;}if(!rejected)throw Error("accepted invalid input");}
console.log("independent acceptance passed");`
	writeInstructionTestFile(t, filepath.Join(dir, "acceptance.js"), source)
	binary := lifecycleBinary(t)
	for _, file := range []string{"check.js", "acceptance.js"} {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		cmd := exec.CommandContext(ctx, binary, "run", file)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", file, err, output)
		}
	}
}
