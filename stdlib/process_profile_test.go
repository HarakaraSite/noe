package stdlib_test

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestProfileCommandHelper(t *testing.T) {
	if os.Getenv("NOE_PROFILE_COMMAND_HELPER") != "1" {
		return
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(8)
	}
	fmt.Fprintf(os.Stdout, "%s|%s", os.Getenv("MARK"), input)
	fmt.Fprint(os.Stderr, "helper stderr")
	os.Exit(7)
}

func TestProfileProcessCapabilityCombinations(t *testing.T) {
	for selection := range 8 {
		info, stdio, commands := selection&1 != 0, selection&2 != 0, selection&4 != 0
		t.Run(fmt.Sprintf("info=%v/stdio=%v/commands=%v", info, stdio, commands), func(t *testing.T) {
			modules := stdlib.Modules{ProcessInfo: info, Stdio: stdio}
			if commands {
				modules.Commands = &stdlib.CommandOptions{}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := stdlib.Execute(ctx, stdlib.Source{Name: "memory:process-selection", Virtual: true, Module: true, Code: fmt.Sprintf(`
const info=%v, stdio=%v, commands=%v;
const api=await import("noe:process").catch(()=>null);
if (Boolean(api)!==(info||stdio||commands)) throw Error("module availability");
if ((typeof process!=="undefined")!==info) throw Error("metadata global selection");
if (api) {
  const expected=["default"];
  if (info) expected.push("process");
  if (stdio) expected.push("stdin","stdout","stderr");
  if (commands) expected.push("exec","execFile","execFileSync","execSync","spawn","spawnSync");
  if (Object.keys(api).sort().join()!==expected.sort().join()) throw Error("selected exports: "+Object.keys(api));
  const named=expected.filter(key=>key!=="default");
  if (Object.keys(api.default).sort().join()!==named.sort().join()) throw Error("namespace shape");
  for (const key of named) if (api[key]!==api.default[key]) throw Error("namespace identity: "+key);
  if ("env" in api.default || "cwd" in api.default) throw Error("metadata flattened into namespace");
  if (info) {
    if (api.process!==globalThis.process || api.process.env.MARK!=="metadata") throw Error("metadata identity");
    for (const key of ["stdin","stdout","stderr"]) {
      if ((key in api.process)!==stdio || (stdio && api.process[key]!==api[key])) throw Error("metadata streams");
    }
    if ("spawn" in api.process || "execFile" in api.process) throw Error("commands leaked into global");
    api.process.exitCode=3;
    if (process.exitCode!==3) throw Error("exitCode identity");
    process.exitCode=0;
  }
}
for (const removed of ["node:child_process","child_process","node:process","process"]) {
  if (await import(removed).then(()=>true,()=>false)) throw Error("removed module: "+removed);
}
`, info, stdio, commands)}, stdlib.Profile{
				Environment: stdlib.Environment{Env: map[string]string{"MARK": "metadata"}}, Modules: modules,
			})
			if err != nil || result.Code != 0 {
				t.Fatalf("capability result=%+v error=%s", result, stdlib.Diagnostic(err))
			}
		})
	}
}

func TestProfileStreamingAndCapturedCommandsAgree(t *testing.T) {
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	encodedProgram, err := json.Marshal(program)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "memory:commands", Virtual: true, Module: true, Code: `
import commands, {spawn, execFile, execFileSync} from "noe:process";
if (spawn!==commands.spawn || commands.execFile!==execFile || commands.execFileSync!==execFileSync)
  throw Error("command namespace differs");
if (typeof process!=="undefined") throw Error("commands installed process metadata");
for (const removed of ["node:child_process", "child_process", "node:process", "process", "noe:io"]) {
  if (await import(removed).then(()=>true,()=>false)) throw Error("unexpected module: "+removed);
}
const program=` + string(encodedProgram) + `;
const args=["-test.run=^TestProfileCommandHelper$"];
const env={NOE_PROFILE_COMMAND_HELPER:"1",MARK:"selected"};
const child=await spawn(program,args,{env});
try {
  if (!(child.stdin instanceof WritableStream) || !(child.stdout instanceof ReadableStream) || !(child.stderr instanceof ReadableStream))
    throw Error("spawn streams");
  const output=new Response(child.stdout).text(), errors=new Response(child.stderr).text();
  const writer=child.stdin.getWriter();
  try { await writer.write(new TextEncoder().encode("雪")); await writer.close(); }
  finally { writer.releaseLock(); }
  const [stdout,stderr,status]=await Promise.all([output,errors,child.exited]);
  if (stdout!=="selected|雪" || stderr!=="helper stderr" || status.code!==7 || status.signal!==null)
    throw Error("streaming output/status");
} finally { await child.close(); await child.close(); }
const captured=await execFile(program,args,{env,input:"雪"}).then(()=>null,error=>error);
if (captured?.code!==7 || captured.stdout!=="selected|雪" || captured.stderr!=="helper stderr")
  throw Error("captured command changed");
let synchronous;
try { execFileSync(program,args,{env,input:"雪"}); } catch (error) { synchronous=error; }
if (synchronous?.code!==7 || synchronous.stdout!==captured.stdout || synchronous.stderr!==captured.stderr)
  throw Error("synchronous command changed");
`}, stdlib.Profile{
		Environment: stdlib.Environment{Cwd: t.TempDir()},
		Modules: stdlib.Modules{Commands: &stdlib.CommandOptions{Allow: func(name string, _ []string) error {
			if name != program {
				return errors.New("unexpected command")
			}
			return nil
		}}},
	})
	if err != nil || result.Code != 0 {
		t.Fatalf("command result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}

func TestProfileCommandAuthorizationIncludesSpawn(t *testing.T) {
	attempts := 0
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "memory:denied-commands", Virtual: true, Module: true, Code: `
import {spawn,execFile} from "noe:process";
for (const run of [spawn,execFile]) {
  const error=await run("denied-command").then(()=>null,error=>error);
  if (!error || !error.message.includes("fixture command denied")) throw Error("command bypassed authorization");
}
`}, stdlib.Profile{Modules: stdlib.Modules{Commands: &stdlib.CommandOptions{Allow: func(string, []string) error {
		attempts++
		return errors.New("fixture command denied")
	}}}})
	if err != nil || result.Code != 0 || attempts != 2 {
		t.Fatalf("authorization result=%+v attempts=%d error=%s", result, attempts, stdlib.Diagnostic(err))
	}
}
