package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
)

func TestParseInstructionOptions(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		args, dirs, operands []string
		err                  string
	}{
		{"default", []string{"script.js"}, nil, []string{"script.js"}, ""},
		{"empty", []string{"--no-instructions", "code"}, []string{}, []string{"code"}, ""},
		{"ordered", []string{"--instruction-dir", "a", "--timeout=1s", "--instruction-dir=b", "--instruction-dir=a", "script", "--no-instructions"}, []string{"a", "b", "a"}, []string{"script", "--no-instructions"}, ""},
		{"literal option path", []string{"--instruction-dir=--literal", "--", "code"}, []string{"--literal"}, []string{"code"}, ""},
		{"terminator", []string{"--", "--no-instructions", "arg"}, nil, []string{"--no-instructions", "arg"}, ""},
		{"missing", []string{"--instruction-dir"}, nil, nil, "requires a directory"},
		{"missing before option", []string{"--instruction-dir", "--no-instructions"}, nil, nil, "requires a directory"},
		{"empty separate", []string{"--instruction-dir", ""}, nil, nil, "requires a directory"},
		{"empty equals", []string{"--instruction-dir="}, nil, nil, "requires a directory"},
		{"conflict first dir", []string{"--instruction-dir=a", "--no-instructions"}, nil, nil, "mutually exclusive"},
		{"conflict first empty", []string{"--no-instructions", "--instruction-dir=a"}, nil, nil, "mutually exclusive"},
		{"duplicate empty", []string{"--no-instructions", "--no-instructions"}, nil, nil, "only once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, operands, err := parseCommandOptions(tc.args)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("error=%v, want %s", err, tc.err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(opts.instructionDirs, tc.dirs) || !reflect.DeepEqual(operands, tc.operands) {
				t.Fatalf("dirs=%#v operands=%#v err=%v", opts.instructionDirs, operands, err)
			}
			if tc.name == "ordered" && opts.timeout != time.Second {
				t.Fatalf("timeout=%s", opts.timeout)
			}
		})
	}
}

func TestCLIInstructionUsageAndBoundary(t *testing.T) {
	for _, command := range []string{"run", "eval"} {
		for _, options := range [][]string{
			{"--instruction-dir"}, {"--instruction-dir="}, {"--instruction-dir", "--no-instructions"},
			{"--instruction-dir=a", "--no-instructions"}, {"--no-instructions", "--instruction-dir=a"},
			{"--no-instructions", "--no-instructions"},
		} {
			var out, stderr bytes.Buffer
			args := append([]string{command}, options...)
			if code := Run(context.Background(), args, nil, &out, &stderr); code != 2 || out.Len() != 0 {
				t.Fatalf("%v: code=%d out=%q err=%q", args, code, &out, &stderr)
			}
		}
	}
	file := filepath.Join(t.TempDir(), "args.js")
	if err := os.WriteFile(file, []byte(`console.log(JSON.stringify(process.argv.slice(2)));`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	args := []string{"run", "--no-instructions", "--", file, "--instruction-dir", "ignored", "--no-instructions"}
	if code := Run(context.Background(), args, nil, &out, &stderr); code != 0 || out.String() != `["--instruction-dir","ignored","--no-instructions"]`+"\n" {
		t.Fatalf("code=%d out=%q err=%q", code, &out, &stderr)
	}
}

func TestCLIInstructionComposition(t *testing.T) {
	cwd, config, scriptDir := t.TempDir(), t.TempDir(), t.TempDir()
	t.Chdir(cwd)
	t.Setenv("XDG_CONFIG_HOME", config)
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(config, "noe", "instructions", "same.md"), "COMMON")
	write(filepath.Join(cwd, ".noe", "instructions", "same.md"), "WORKSPACE")
	write(filepath.Join(cwd, "one", "same.md"), "ONE")
	write(filepath.Join(cwd, "two", "same.md"), "TWO")
	write(filepath.Join(scriptDir, "one", "same.md"), "WRONG_SCRIPT_DIR")
	write(filepath.Join(cwd, "cwd-proof"), "cwd-ok")
	var requests = make(chan string, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		var system string
		for _, msg := range body.Messages {
			if msg.Role == "system" {
				system += msg.Content
			}
		}
		requests <- system
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"fixture/model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	agentFixtureSettings(t, server.URL)
	source := fmt.Sprintf(`import {run_agent} from "noe:agent";
import {readFileSync} from "node:fs";
if(process.cwd()!==%q || readFileSync("cwd-proof","utf8")!=="cwd-ok") throw Error("cwd changed");
const r=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",system:"EXPLICIT_SYSTEM"},"hello");
if(r.lastResult?.state!=="completed") throw Error(JSON.stringify(r));`, cwd)
	file := filepath.Join(scriptDir, "agent.js")
	write(file, source)
	facts := agent.Facts() + "\n\n" + agent.URLImportFacts("enabled")
	for _, command := range []string{"run", "eval"} {
		for _, tc := range []struct {
			name    string
			options []string
			extra   string
		}{
			{"default", nil, "COMMON\n\nWORKSPACE"},
			{"replacement", []string{"--instruction-dir", "two", "--timeout=10s", "--instruction-dir=one", "--instruction-dir=two", "--instruction-dir=missing"}, "TWO\n\nONE\n\nTWO"},
			{"empty", []string{"--no-instructions"}, ""},
		} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				operand := file
				if command == "eval" {
					operand = source
				}
				args := append([]string{command}, tc.options...)
				args = append(args, operand)
				var out, stderr bytes.Buffer
				code := Run(context.Background(), args, nil, &out, &stderr)
				if code != 0 || out.Len() != 0 || stderr.Len() != 0 {
					t.Fatalf("code=%d out=%q err=%q", code, &out, &stderr)
				}
				var got string
				select {
				case got = <-requests:
				case <-time.After(5 * time.Second):
					t.Fatal("provider request not received")
				}
				want := facts
				if tc.extra != "" {
					want += "\n\n" + tc.extra
				}
				want += "\n\nEXPLICIT_SYSTEM"
				if got != want {
					t.Fatalf("system mismatch\ngot: %q\nwant: %q", got, want)
				}
			})
		}
	}
}

func TestCLISelectedInstructionsRemainLazy(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.WriteFile("not-dir", []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"run", "eval"} {
		codeText := `console.log("ordinary");`
		file := filepath.Join(cwd, "plain.js")
		if err := os.WriteFile(file, []byte(codeText), 0600); err != nil {
			t.Fatal(err)
		}
		operand := file
		if command == "eval" {
			operand = codeText
		}
		var out, stderr bytes.Buffer
		code := Run(context.Background(), []string{command, "--instruction-dir", "not-dir", operand}, nil, &out, &stderr)
		if code != 0 || out.String() != "ordinary\n" || stderr.Len() != 0 {
			t.Fatalf("%s: code=%d out=%q err=%q", command, code, &out, &stderr)
		}
	}
}
