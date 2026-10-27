package runtime

import (
	"context"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/internal/noeprofile"
	"os"
	"path/filepath"
	"testing"
)

func TestModelCatalogSnapshotWithoutCredentials(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte(`{"providers":{"openrouter":{"keyFile":"/missing-key","models":{"deepseek/deepseek-v4.1-flash":{"catalog":{"description":"local selection policy"}}}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	opts := Options{Agent: &noeprofile.AgentOptions{Models: model.Options{ConfigFile: config}}}
	source := `import {list_models} from "noe:agent";
const all=await list_models();
if(all.length!==15 || all.some(m=>"keyFile" in m || "baseURL" in m))throw Error("catalog shape");
for(const provider of ["openai-chatgpt","openai-api"]){
 const models=await list_models({provider,kind:"agent"});
 if(!models.some(m=>m.model==="gpt-6.1-terra"))throw Error("Terra missing from " + provider);
}
const images=await list_models({kind:"image"});if(images.length!==2)throw Error("image catalog");
const agents=await list_models({provider:"openrouter",kind:"agent"});
if(agents.length!==2 || agents[0].description!=="local selection policy")throw Error("external override");
for(const bad of [null,[],{kind:"other"},{provider:""},{unexpected:true}]){
 let rejected=false;try{await list_models(bad);}catch(e){if(!(e instanceof TypeError))throw e;rejected=true;}
 if(!rejected)throw Error("invalid query accepted");
}`
	source += `
const decisions=await list_models({kind:"decision"});
if(decisions.length!==3)throw Error("decision catalog");
const {writeFileSync}=await import("node:fs");
writeFileSync(` + jsString(t, config) + `,"invalid");
if((await list_models({kind:"decision"})).length!==3)throw Error("catalog snapshot changed");`
	code, err := Execute(context.Background(), Source{filepath.Join(dir, "snapshot.js"), source}, opts)
	if code != 0 || err != nil {
		t.Fatalf("snapshot code=%d err=%v", code, err)
	}
}
