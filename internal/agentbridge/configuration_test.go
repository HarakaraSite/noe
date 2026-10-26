package agentbridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestAgentConfigurationNormalizationPreservesContextOptions(t *testing.T) {
	const inherited = `{"contextBudget":{"inputTokens":4096},"contextPolicy":{"retainRatio":0.3,"responseReserveTokens":128},"executionLimits":{"providerCalls":2}}`
	for _, test := range []struct {
		name, defaults, config, want string
		apiError, taskError          string
	}{
		{name: "absent", defaults: "{}", config: "{}", want: "{}"},
		{name: "inherited", defaults: inherited, config: "{}", want: inherited},
		{name: "undefined inherits", defaults: inherited, config: "{contextBudget:undefined,contextPolicy:undefined,executionLimits:undefined}", want: inherited},
		{name: "explicit override", defaults: inherited,
			config: `{contextBudget:{inputTokens:2048},contextPolicy:{retainRatio:0.2,responseReserveTokens:64},executionLimits:{providerCalls:0}}`,
			want:   `{"contextBudget":{"inputTokens":2048},"contextPolicy":{"retainRatio":0.2,"responseReserveTokens":64},"executionLimits":{"providerCalls":0}}`},
		{name: "null clears and empty limits override", defaults: inherited,
			config: "{contextBudget:null,contextPolicy:null,executionLimits:{}}",
			want:   `{"contextBudget":null,"contextPolicy":null,"executionLimits":{}}`},
		{name: "empty policy is not absent", defaults: "{}", config: "{contextPolicy:{}}", want: `{"contextPolicy":{}}`},
		{name: "invalid budget", defaults: "{}", config: "{contextBudget:{inputTokens:0}}",
			apiError: "positive safe integer", taskError: "positive safe integer"},
		{name: "invalid policy", defaults: "{}", config: "{contextPolicy:{retainRatio:1}}",
			apiError: "between 0 and 1", taskError: "between 0 and 1"},
		{name: "null execution limits rejected by API", defaults: "{}", config: "{executionLimits:null}", want: "{}",
			apiError: "executionLimits must be an object"},
		{name: "inherited values are validated", defaults: `{"contextBudget":{"inputTokens":0}}`, config: "{}",
			apiError: "positive safe integer", taskError: "positive safe integer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rt := quickjs.New()
			defer rt.Close()
			rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
				source, ok := moduleSource(specifier)
				if !ok {
					return "", "", fmt.Errorf("unexpected module %q", specifier)
				}
				return source, specifier, nil
			})
			var publicConfig, taskConfig string
			err := rt.SetModule("noe:agent/internal", map[string]any{
				"defaults":     func() string { return test.defaults },
				"instructions": func() string { return "" },
				"request": func(method, payload string) (string, error) {
					switch method {
					case "spawn_agent":
						var request struct {
							Config json.RawMessage `json:"config"`
						}
						if err := json.Unmarshal([]byte(payload), &request); err != nil {
							return "", err
						}
						publicConfig = string(request.Config)
						return `{"agentId":"fixture","commandId":"command"}`, nil
					case "start":
						taskConfig = payload
						// Stop after the real task module has normalized its config,
						// before starting a model loop or contacting any provider.
						return "", errors.New("normalization fixture reached start")
					default:
						return "", fmt.Errorf("unexpected request %q", method)
					}
				},
				"beginHookScope":    func() {},
				"requestHook":       func() {},
				"endHookScope":      func() {},
				"resolveHookModule": func() {},
			})
			if err != nil {
				t.Fatal(err)
			}
			taskError := test.taskError
			if taskError == "" {
				taskError = "normalization fixture reached start"
			}
			_, err = rt.EvalModule("[configuration normalization]", fmt.Sprintf(`
import {spawn_agent} from "noe:agent";
import {executeTask} from "noe:agent/task";
const config={provider:"fixture",model:"fixture/model",effort:"none",...%s};
async function check(operation,expectedError){
  let failure;
  try{await operation()}catch(error){failure=error}
  if(expectedError){
    if(!failure||!String(failure).includes(expectedError))throw Error("unexpected validation result: "+String(failure));
  }else if(failure){throw failure}
}
await check(()=>spawn_agent(config,"task"),%q);
await check(()=>executeTask(config,"task"),%q);
`, test.config, test.apiError, taskError))
			if err != nil {
				t.Fatal(err)
			}
			if test.apiError == "" {
				assertNormalizedContextFields(t, publicConfig, test.want, false)
			} else if publicConfig != "" {
				t.Fatalf("invalid public config reached admission: %s", publicConfig)
			}
			if test.taskError == "" {
				assertNormalizedContextFields(t, taskConfig, test.want, true)
			} else if taskConfig != "" {
				t.Fatalf("invalid task config reached start: %s", taskConfig)
			}
		})
	}
}

func assertNormalizedContextFields(t *testing.T, raw, want string, task bool) {
	t.Helper()
	var actual, expected map[string]any
	if err := json.Unmarshal([]byte(raw), &actual); err != nil {
		t.Fatalf("invalid captured config %q: %v", raw, err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if task {
		delete(expected, "executionLimits") // Limits belong to admission, not task start.
		if _, present := expected["contextPolicy"]; !present {
			expected["contextPolicy"] = map[string]any{"retainRatio": 0.1}
		}
	}
	selected := make(map[string]any)
	for _, key := range []string{"contextBudget", "contextPolicy", "executionLimits"} {
		if value, present := actual[key]; present {
			selected[key] = value
		}
	}
	if !reflect.DeepEqual(selected, expected) {
		t.Fatalf("normalized fields=%v, want %v (task=%v)", selected, expected, task)
	}
}
