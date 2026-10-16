#!/usr/bin/env python3
"""Run the four demos and retain their outputs and host event traces."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
from datetime import datetime, timezone


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def output_object(text):
    """The agent returns text; accept JSON surrounded by model commentary."""
    decoder = json.JSONDecoder()
    for i, char in enumerate(text):
        if char == "{":
            try:
                value, _ = decoder.raw_decode(text[i:])
                if isinstance(value, dict):
                    return value
            except json.JSONDecodeError:
                pass
    raise ValueError("final answer contains no JSON object")


def check(name, stdout, events):
    value = output_object(stdout)
    starts = [e for e in events if e["kind"] == "run_start"]
    ends = [e for e in events if e["kind"] == "run_end"]
    tools = [e for e in events if e["kind"] == "js_end"]
    models = [e for e in events if e["kind"] == "model_end"]
    assert events[-1]["kind"] == "host_end", "host did not finish"
    assert starts and len(starts) == len(ends), "unmatched agent starts/ends"
    assert all(e["status"] == "completed" for e in ends), "an agent failed"
    assert models and all(e["status"] == "ok" for e in models), "model call failed"
    children_overlap = None
    if name in ("single", "recovery"):
        assert {key: value.get(key) for key in ("count", "total", "average")} == {
            "count": 4, "total": 100, "average": 25
        }, value
        if name == "recovery":
            failures = [i for i, e in enumerate(tools) if e["exitCode"] != 0]
            assert failures and any(e["exitCode"] == 0 for e in tools[failures[0]+1:]), "no failed child process followed by recovery"
    elif name == "delegate":
        assert value.get("total") == 100 and sorted(p["total"] for p in value["parts"]) == [30, 70], value
        children = [e for e in starts if e["parentRunId"]]
        assert len(children) == 2, "expected two child agents"
        assert {e["name"] for e in children} == {"left", "right"}, "unexpected child names"
        by_id = {e["runId"]: e for e in ends}
        assert all(any(tool["runId"] == child["runId"] for tool in tools) for child in children), "child did not execute JS"
        children_overlap = max(e["time"] for e in children) < min(by_id[e["runId"]]["time"] for e in children)
        assert children_overlap, "child agent lifetimes did not overlap"
    else:
        assert "fixture" in value.get("source", ""), value
        assert any(e["kind"] == "service_search" for e in events), "host service was not called"
        assert "42" in json.dumps(value["results"]), value
    usage = [e.get("usage") or {} for e in models]
    return {
        "final_json": value,
        "agents": len(starts),
        "model_calls": len(models),
        "js_calls": len(tools),
        "failed_js_calls": sum(e["exitCode"] != 0 for e in tools),
        "children_overlap": children_overlap,
        "prompt_tokens": sum(u.get("prompt_tokens", 0) for u in usage),
        "completion_tokens": sum(u.get("completion_tokens", 0) for u in usage),
        "cached_tokens": sum((u.get("prompt_tokens_details") or {}).get("cached_tokens", 0) for u in usage),
        "reported_cost_usd": sum(u.get("cost", 0) for u in usage),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", required=True, type=Path)
    parser.add_argument("--noe", required=True, type=Path)
    parser.add_argument("--provider", choices=("mock", "openrouter"), default="mock")
    parser.add_argument("--model", default="deepseek/deepseek-v4.1-flash")
    parser.add_argument("--key-file", type=Path)
    parser.add_argument("--output", type=Path, required=True, help="new output directory")
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)
    root = Path(__file__).resolve().parent
    files = sorted(list(root.glob("*.go")) + list(root.glob("lib/*.js")) + list(root.glob("examples/*.js")) + list(root.glob("fixtures/*.json")) + [root/"go.mod", root/"go.sum", root/"verify.py"])
    result = {
        "started_at": datetime.now(timezone.utc).isoformat(),
        "provider": args.provider, "model": args.model,
        "platform": platform.platform(),
        "go_version": subprocess.check_output(["go", "version"], text=True).strip(),
        "base_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip(),
        "host_sha256": digest(args.host), "noe_sha256": digest(args.noe),
        "source_sha256": {str(p.relative_to(root)): digest(p) for p in files},
        "scenarios": {},
    }
    for name in ("single", "recovery", "delegate", "service"):
        trace = (args.output / (name + ".jsonl")).resolve()
        command = [str(args.host.resolve()), "--noe", str(args.noe.resolve()),
                   "--provider", args.provider, "--model", args.model,
                   "--max-steps", "6", "--max-tokens", "4096", "--timeout", "120s",
                   "--trace", str(trace)]
        if args.key_file:
            command += ["--key-file", str(args.key_file.expanduser().resolve())]
        command += ["demo", name]
        print(f"{args.provider}: {name} ...", flush=True)
        run = subprocess.run(command, cwd=root, stdin=subprocess.DEVNULL, capture_output=True, text=True)
        (args.output / (name + ".stdout")).write_text(run.stdout)
        (args.output / (name + ".stderr")).write_text(run.stderr)
        entry = {"exit_code": run.returncode}
        try:
            assert run.returncode == 0, run.stderr
            events = [json.loads(line) for line in trace.read_text().splitlines()]
            entry.update(check(name, run.stdout, events))
            entry["passed"] = True
        except (AssertionError, ValueError, KeyError, OSError) as error:
            entry.update(passed=False, error=str(error))
        result["scenarios"][name] = entry
        print(json.dumps(entry, ensure_ascii=False), flush=True)
    result["finished_at"] = datetime.now(timezone.utc).isoformat()
    result["passed"] = all(s["passed"] for s in result["scenarios"].values())
    (args.output / "results.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
