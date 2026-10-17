//go:build e2e && linux

package e2e_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestREPLTerminalTabCompletionUsesLiveSessionWithoutCallingGetter(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "completion.txt"), []byte("NOE_COMPLETION_FILE_VALUE"), 0o600); err != nil {
		t.Fatalf("write completion fixture: %v", err)
	}
	session := startTerminalREPL(t, work, nil)

	session.enter(t, `const fs = await import("node:fs/promises")`)
	session.writeKeys(t, `await fs.readF`+"\t"+`("completion.txt","utf8").then(text => console.log("NOE_COMPLETION:" + text))`+"\r")
	session.waitForOutputAndPrompt(t, "NOE_COMPLETION:NOE_COMPLETION_FILE_VALUE")
	session.writeKeys(t, `await fs.readd`+"\t"+`(".").then(entries => console.log("DIRECTORY_COMPLETION:" + entries.includes("completion.txt")))`+"\r")
	session.waitForOutputAndPrompt(t, "DIRECTORY_COMPLETION:true")
	session.writeKeys(t, `await fs.stat`+"\t"+`("completion.txt").then(info => console.log("STAT_COMPLETION:" + info.isFile()))`+"\r")
	session.waitForOutputAndPrompt(t, "STAT_COMPLETION:true")

	session.writeKeys(t, `console.lo`+"\t"+`("COMPLETION_CONSOLE")`+"\r")
	session.waitForOutputAndPrompt(t, "COMPLETION_CONSOLE")

	session.enter(t, `const e2eUniqueVariable = "USER_VARIABLE_COMPLETED"`)
	session.writeKeys(t, "console.log(e2eUniqueV\t)\r")
	session.waitForOutputAndPrompt(t, "USER_VARIABLE_COMPLETED")

	session.enter(t, `const e2eUniqueObject = {e2eUniqueField: "OBJECT_FIELD_COMPLETED"}`)
	session.writeKeys(t, "console.log(e2eUniqueObject.e2eUniqueF\t)\r")
	session.waitForOutputAndPrompt(t, "OBJECT_FIELD_COMPLETED")

	session.enter(t, "const 商品情報 = {税額: 42}")
	session.writeKeys(t, "console.log(\"JAPANESE_COMPLETION:\" + 商品情\t.税\t)\r")
	session.waitForOutputAndPrompt(t, "JAPANESE_COMPLETION:42")

	session.enter(t, "let getterReads = 0")
	session.enter(t, `const getterTarget = { get zzE2EGetter() { getterReads++; return "getter value"; } }`)
	beforeGetterCompletion := session.stdout.snapshot()
	session.writeKeys(t, "try { getterTarget.zzE2E\t")
	session.stdout.waitFor(t, "getter property completion", func(output string) bool {
		if len(output) < len(beforeGetterCompletion) {
			return false
		}
		return strings.Contains(output[len(beforeGetterCompletion):], "getterTarget.zzE2EGetter")
	})
	beforeGetterAssignment := session.stdout.snapshot()
	session.writeKeys(t, "\x01\x0b = 0; } catch (error) {}\r")
	session.waitForPromptAfter(t, beforeGetterAssignment)
	session.writeKeys(t, `console.log("GETTER_READS:" + getterReads)`+"\r")
	session.waitForOutputAndPrompt(t, "GETTER_READS:0")

	session.enter(t, "let proxyTrapCalls = 0")
	session.enterMultiline(t, []string{
		"const completionProxy = new Proxy({}, {",
		`  ownKeys() { proxyTrapCalls++; throw new Error("completion proxy trap"); },`,
		"})",
	})
	beforeProxyCompletion := session.stdout.snapshot()
	session.writeKeys(t, "completionProxy.\t\x01\x0b"+`console.log("PROXY_TRAP_COUNT:" + proxyTrapCalls)`+"\r")
	session.stdout.waitFor(t, "completion failure recovery and proxy trap", func(output string) bool {
		for _, candidate := range strings.Split(normalizePTYOutput(output), "\n") {
			candidate = strings.TrimRight(candidate, "\r")
			if !strings.HasPrefix(candidate, "PROXY_TRAP_COUNT:") {
				continue
			}
			count, err := strconv.Atoi(strings.TrimPrefix(candidate, "PROXY_TRAP_COUNT:"))
			return err == nil && count > 0
		}
		return false
	})
	session.waitForPromptAfter(t, beforeProxyCompletion)

	session.writeKeys(t, "\x04")
	result := session.finish(t, 0)
	assertNoPTYOutputLine(t, result.stdout, "GETTER_READS:1")
	assertNoPTYOutputLine(t, result.stdout, "PROXY_TRAP_COUNT:0")
}

func TestREPLTerminalCompletionProxyExitRestoresSettings(t *testing.T) {
	session := startTerminalREPL(t, t.TempDir(), nil)
	session.enter(t, `const completionExitProxy = new Proxy({}, { ownKeys() { console.log('COMPLETION_EXIT_TRAP'); process.exit(7); } })`)
	beforeCompletion := session.stdout.snapshot()
	session.writeKeys(t, "completionExitProxy.\t")
	session.stdout.waitFor(t, "completion exit trap marker", func(output string) bool {
		if len(output) < len(beforeCompletion) {
			return false
		}
		return strings.Contains(output[len(beforeCompletion):], "COMPLETION_EXIT_TRAP")
	})
	session.finish(t, 7)
}
