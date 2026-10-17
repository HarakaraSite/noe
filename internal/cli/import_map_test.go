package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestImportMapCLI(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"maps/map.json":   `{"imports":{"app/value":"../lib/value.mjs"}}`,
		"lib/value.mjs":   `export const value=42;`,
		"scripts/main.js": `import {value} from "app/value"; console.log(value,JSON.stringify(process.argv.slice(2)));`,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	for _, tc := range []struct {
		args        []string
		input, want string
	}{
		{[]string{"run", "--import-map", "maps/map.json", "scripts/main.js", "--import-map=unchanged"}, "", "42 [\"--import-map=unchanged\"]\n"},
		{[]string{"eval", "--import-map=maps/map.json", `import {value} from "app/value";console.log(value)`}, "", "42\n"},
		{[]string{"repl", "--import-map=maps/map.json"}, "const lib=await import(\"app/value\")\nlib.value\n", "> > 42\n> "},
	} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), tc.args, strings.NewReader(tc.input), &out, &errOut)
		if code != 0 || out.String() != tc.want || errOut.Len() != 0 {
			t.Fatalf("args=%v code=%d out=%q err=%q", tc.args, code, out.String(), errOut.String())
		}
	}
}

func TestImportMapCLIValidationAndOptionBoundaries(t *testing.T) {
	for _, args := range [][]string{{"--import-map"}, {"--import-map="}, {"--import-map", "--observe"}, {"--import-map=a", "--import-map=b"}} {
		_, _, err := parseCommandOptions(args)
		if err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, args := range [][]string{{"script.js", "--import-map=a"}, {"--", "--import-map=a"}} {
		opts, operands, err := parseCommandOptions(args)
		want := args
		if args[0] == "--" {
			want = args[1:]
		}
		if err != nil || opts.importMap != "" || !reflect.DeepEqual(operands, want) {
			t.Fatalf("args=%v opts=%+v operands=%v err=%v", args, opts, operands, err)
		}
	}
	bad := filepath.Join(t.TempDir(), "map.json")
	if err := os.WriteFile(bad, []byte(`{"imports":{"fs":"./never-read.js"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"eval", "repl"} {
		args := []string{command, "--import-map", bad}
		if command == "eval" {
			args = append(args, `console.log("must not execute")`)
		}
		var out, errOut bytes.Buffer
		code := Run(context.Background(), args, strings.NewReader(""), &out, &errOut)
		if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "reserved") {
			t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
		}
	}
}
