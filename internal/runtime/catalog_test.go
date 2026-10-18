package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noe/internal/agent"
	"noe/internal/agent/provider"
)

func TestModelCatalogInJSAndREPLWithoutCredentials(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte(`{"providers":{"openrouter":{"keyFile":"/missing-key","models":{"deepseek/deepseek-v4.1-flash":{"catalog":{"description":"local selection policy"}}}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	opts := Options{Agent: &agent.Options{ProviderOptions: provider.Options{ConfigFile: config}}}
	source := `import {list_models} from "noe:agent";
const all=await list_models();
if(all.length!==13 || all.some(m=>"keyFile" in m || "baseURL" in m))throw Error("catalog shape");
const images=await list_models({kind:"image"});if(images.length!==2)throw Error("image catalog");
const agents=await list_models({provider:"openrouter",kind:"agent"});
if(agents.length!==2 || agents[0].description!=="local selection policy")throw Error("external override");
for(const bad of [null,[],{kind:"other"},{provider:""},{unexpected:true}]){
 let rejected=false;try{await list_models(bad);}catch(e){if(!(e instanceof TypeError))throw e;rejected=true;}
 if(!rejected)throw Error("invalid query accepted");
}`
	code, err := Execute(context.Background(), Source{filepath.Join(dir, "main.js"), source}, opts)
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	s, err := NewSession(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, input := range []string{
		`const catalogModule=await import("noe:agent")`,
		`const list_models=catalogModule.list_models`,
		`const first=await list_models({kind:"decision"})`,
		`first.length`,
	} {
		got := s.Eval(context.Background(), input)
		if got.Err != nil || got.Exit {
			t.Fatalf("input=%q result=%+v", input, got)
		}
		if input == "first.length" && got.Output != "3" {
			t.Fatalf("result=%+v", got)
		}
	}
	if err := os.WriteFile(config, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	got := s.Eval(context.Background(), `await list_models({kind:"decision"}).then(v=>v.length)`)
	if got.Err != nil || strings.TrimSpace(got.Output) != "3" {
		t.Fatalf("REPL reread configuration: %+v", got)
	}
}
