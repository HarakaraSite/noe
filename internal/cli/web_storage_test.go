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

func TestLocalStorageCLIOptions(t *testing.T) {
	for _, args := range [][]string{
		{"--local-storage"}, {"--local-storage="}, {"--local-storage", "--observe"},
		{"--local-storage=a", "--local-storage=b"}, {"--local-storage=a\x00b"},
	} {
		if _, _, err := parseCommandOptions(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
	for _, args := range [][]string{{"script.js", "--local-storage=a"}, {"--", "--local-storage=a"}} {
		options, operands, err := parseCommandOptions(args)
		want := args
		if args[0] == "--" {
			want = args[1:]
		}
		if err != nil || options.localStorage != "" || !reflect.DeepEqual(operands, want) {
			t.Fatalf("args=%q options=%+v operands=%q err=%v", args, options, operands, err)
		}
	}
}

func TestLocalStorageCLIRunAndEvalShareExplicitFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const code = `
if (sessionStorage.length !== 0) throw Error("session survived an execution");
sessionStorage.setItem("temporary", "value");
const count = Number(localStorage.getItem("count") ?? "0") + 1;
localStorage.setItem("count", String(count));
console.log(count, JSON.stringify(process.argv.slice(2)));`
	if err := os.WriteFile(filepath.Join(dir, "counter.js"), []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "--local-storage", "state.json", "counter.js", "--local-storage=script-argument"}, "1 [\"--local-storage=script-argument\"]\n"},
		{[]string{"eval", "--local-storage=state.json", code}, "2 []\n"},
		{[]string{"eval", `console.log(typeof localStorage, sessionStorage.length)`}, "undefined 0\n"},
	} {
		var out, errOut bytes.Buffer
		status := Run(context.Background(), tc.args, strings.NewReader(""), &out, &errOut)
		if status != 0 || out.String() != tc.want || errOut.Len() != 0 {
			t.Fatalf("args=%q code=%d out=%q err=%q", tc.args, status, out.String(), errOut.String())
		}
	}
}
