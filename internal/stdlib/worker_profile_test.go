package stdlib

import (
	"slices"
	"testing"
)

func TestChildConfigCarriesEnvironmentWithoutProcessInfo(t *testing.T) {
	parentEnv := map[string]string{"PARENT": "yes"}
	host := &workerHost{cfg: Config{
		environmentCwd:  "/virtual",
		environmentArgs: []string{"node", "parent.mjs"},
		environmentEnv:  parentEnv,
	}}
	workerEnv := map[string]string{"WORKER": "yes"}
	child := host.childConfig(&workerContext{}, "ignored", workerOptions{
		Eval: true, Argv: []string{"child-arg"}, Env: workerEnv, HasEnv: true,
	})
	cwd, args, env := ExecutionEnvironment(child)
	if cwd != "/virtual" {
		t.Fatalf("worker cwd = %q", cwd)
	}
	wantArgs := []string{"node", "[worker eval]", "child-arg"}
	if !slices.Equal(args, wantArgs) {
		t.Fatalf("worker args = %#v, want %#v", args, wantArgs)
	}
	if len(env) != 1 || env["WORKER"] != "yes" || child.Process != nil {
		t.Fatalf("worker environment/process = %#v/%#v", env, child.Process)
	}
	workerEnv["WORKER"] = "mutated"
	parentEnv["PARENT"] = "mutated"
	_, _, env = ExecutionEnvironment(child)
	if env["WORKER"] != "yes" || env["PARENT"] != "" {
		t.Fatalf("worker environment shared mutable input: %#v", env)
	}
}

func TestLegacyChildConfigInheritsAndCopiesProcessEnvironment(t *testing.T) {
	parentEnv := map[string]string{"PARENT": "yes"}
	host := &workerHost{cfg: Config{Process: &Process{
		Args: []string{"node", "parent.mjs"},
		Env:  parentEnv,
	}}}

	child := host.childConfig(&workerContext{}, "ignored", workerOptions{Eval: true})
	if child.Process == nil || child.Process.Env["PARENT"] != "yes" {
		t.Fatalf("omitted worker env did not inherit the parent: %#v", child.Process)
	}
	if got := child.Process.Args[0]; got != "node" {
		t.Fatalf("worker executable = %q, want parent executable node", got)
	}
	child.Process.Env["PARENT"] = "child mutation"
	if parentEnv["PARENT"] != "yes" {
		t.Fatalf("inherited environment shares the parent's map: %#v", parentEnv)
	}

	workerEnv := map[string]string{"WORKER": "override"}
	overridden := host.childConfig(&workerContext{}, "ignored", workerOptions{Eval: true, Env: workerEnv, HasEnv: true})
	if len(overridden.Process.Env) != 1 || overridden.Process.Env["WORKER"] != "override" {
		t.Fatalf("worker env override = %#v", overridden.Process.Env)
	}
	workerEnv["WORKER"] = "caller mutation"
	overridden.Process.Env["WORKER"] = "worker mutation"
	if workerEnv["WORKER"] != "caller mutation" || parentEnv["PARENT"] != "yes" {
		t.Fatalf("override environment was not independent: options=%#v parent=%#v", workerEnv, parentEnv)
	}
}
