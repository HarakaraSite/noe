package stdlib_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestProfileStdioNamespaceSharesWebStreams(t *testing.T) {
	for _, processInfo := range []bool{false, true} {
		t.Run(fmt.Sprint("process-info=", processInfo), func(t *testing.T) {
			var output, diagnostics bytes.Buffer
			input := []byte{0, 255, 128, 10}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := stdlib.Execute(ctx, stdlib.Source{Name: "memory:stdio", Virtual: true, Module: true, Code: `
import selected, {stdin,stdout,stderr} from "noe:process";
if (selected.stdin!==stdin || selected.stdout!==stdout || selected.stderr!==stderr)
  throw Error("default and named streams differ");
if (!(stdin instanceof ReadableStream) || !(stdout instanceof WritableStream) || !(stderr instanceof WritableStream))
  throw Error("stdio is not Web Streams");
if (typeof stdin.read!=="undefined" || typeof stdout.write!=="undefined" || "isTTY" in stdout)
  throw Error("removed synchronous facade survived");
const withInfo=` + fmt.Sprint(processInfo) + `;
if (withInfo) {
  if (process!==selected.process || process.stdin!==stdin || process.stdout!==stdout || process.stderr!==stderr || process.env.MARK!=="selected")
    throw Error("global process streams differ");
} else if (typeof process!=="undefined" || ["process","argv","env","cwd","pid"].some(key=>key in selected)) {
  throw Error("stdio installed process metadata");
}
if (await import("noe:io").then(()=>true,()=>false)) throw Error("removed module imported");
await stdin.pipeTo(stdout,{preventClose:true});
const writer=stdout.getWriter();
try { await writer.write(new TextEncoder().encode("tail")); } finally { writer.releaseLock(); }
const errorWriter=stderr.getWriter();
try { await errorWriter.write(new TextEncoder().encode("diagnostic")); } finally { errorWriter.releaseLock(); }
await stdout.close();
console.log("console after endpoint close");
`}, stdlib.Profile{
				Environment: stdlib.Environment{Env: map[string]string{"MARK": "selected"},
					Stdin: bytes.NewReader(input), Stdout: &output, Stderr: &diagnostics},
				Modules: stdlib.Modules{ProcessInfo: processInfo, Stdio: true},
			})
			want := append(append([]byte{}, input...), []byte("tailconsole after endpoint close\n")...)
			if err != nil || result.Code != 0 || !bytes.Equal(output.Bytes(), want) || diagnostics.String() != "diagnostic" {
				t.Fatalf("stdio result=%+v output=%q diagnostics=%q error=%s", result, output.Bytes(), diagnostics.String(), stdlib.Diagnostic(err))
			}
		})
	}
}

func TestProfileMetadataDoesNotSelectStdio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "memory:metadata", Virtual: true, Module: true, Code: `
import {process as processInfo} from "noe:process";
const module=await import("noe:process");
if (processInfo!==process || process.env.MARK!=="metadata") throw Error("process metadata");
for (const key of ["stdin","stdout","stderr"]) {
  if (key in module || key in processInfo) throw Error("unselected stream installed: "+key);
}
`}, stdlib.Profile{Environment: stdlib.Environment{Env: map[string]string{"MARK": "metadata"}}, Modules: stdlib.Modules{ProcessInfo: true}})
	if err != nil || result.Code != 0 {
		t.Fatalf("metadata result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	result, err = stdlib.Execute(ctx, stdlib.Source{Name: "memory:no-process", Virtual: true, Module: true, Code: `
if (typeof process!=="undefined") throw Error("unselected metadata global");
if (await import("noe:process").then(()=>true,()=>false)) throw Error("unselected process module");
`}, stdlib.Profile{Environment: stdlib.Environment{Stdin: strings.NewReader("unselected")}})
	if err != nil || result.Code != 0 {
		t.Fatalf("unselected result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}
