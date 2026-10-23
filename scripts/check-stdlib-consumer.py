#!/usr/bin/env python3
"""Check the public Go package from separate modules without publishing it."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import zipfile

MODULE = "forge.harakara.site/littleisland/noe/v2"
VERSION = "v2.0.0-dev.0"
ENGINE = "github.com/go-quickjs/go-quickjs"
ENGINE_VERSION = "v0.28.1-noe.1"
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--cross", action="store_true", help="compile consumer for six release targets")
    parser.add_argument("--agent", action="store_true", help="check the public agent consumer instead of the generic stdlib consumer")
    args = parser.parse_args()
    package = "agent" if args.agent else "stdlib"
    modes = [["portable"], ["gohost"]] if args.agent else [[]]
    output = args.output.resolve()
    if output == ROOT or ROOT in output.parents:
        raise ValueError("--output must be outside the checkout")
    output.mkdir(parents=True, exist_ok=False)
    env = {k: v for k, v in os.environ.items() if not k.startswith("NOE_")}
    env.update(GOWORK="off", CGO_ENABLED="0", GOFLAGS="")
    host = json.loads(subprocess.check_output(["go", "env", "-json", "GOHOSTOS", "GOHOSTARCH", "GOMODCACHE"], env=env))
    env.update(GOOS=host["GOHOSTOS"], GOARCH=host["GOHOSTARCH"])
    records = []

    def command(name, argv, cwd, settings=env):
        start = time.monotonic()
        with (output / (name + ".log")).open("w") as log:
            result = subprocess.run(argv, cwd=cwd, env=settings, stdout=log, stderr=subprocess.STDOUT, timeout=300)
        record = dict(name=name, command=argv, exit_code=result.returncode, seconds=round(time.monotonic() - start, 3))
        records.append(record)
        (output / "results.json").write_text(json.dumps(records, indent=2) + "\n")
        print(json.dumps(record), flush=True)
        if result.returncode:
            raise RuntimeError((output / (name + ".log")).read_text())

    def consumer(name, replacement):
        folder = output / name
        folder.mkdir()
        for source in (ROOT / package / "testdata/consumer").glob("*.go"):
            shutil.copyfile(source, folder / source.name)
        text = f"module example.com/noe-{package}-consumer\n\ngo 1.27.2\n\nrequire (\n {MODULE} {VERSION}\n github.com/go-quickjs/go-quickjs v0.28.0\n)\n"
        if replacement:
            text += f"\nreplace {MODULE} => {json.dumps(str(ROOT))}\n"
            engine_source = ROOT / "third_party/go-quickjs"
            if engine_source.is_dir():
                text += f"replace {ENGINE} => {json.dumps(str(engine_source))}\n"
        (folder / "go.mod").write_text(text)
        shutil.copyfile(ROOT / "go.sum", folder / "go.sum")
        return folder

    # Only explicit tracked source and known implementation additions enter the zip.
    tracked = subprocess.check_output(["git", "ls-files", "-z"], cwd=ROOT).decode().split("\0")
    names = {name for name in tracked if name}
    for directory in ["stdlib", "agent", "internal", "examples/agent-embedding"]:
        folder = ROOT / directory
        if folder.exists():
            # New Go sources and bridge-owned embedded JS are explicit additions.
            names.update(str(p.relative_to(ROOT)) for p in folder.rglob("*")
                         if p.is_file() and p.suffix in {".go", ".js", ".mjs", ".md"})
    for name in ["docs/go-agent-embedding.md", "examples/agent-embedding/go.mod.template"]:
        if (ROOT / name).is_file():
            names.add(name)
    names.update(["scripts/check-stdlib-consumer.py", "docs/go-embedding.md", "examples/embedding/go.mod.template"])
    nested = [str(Path(name).parent) + "/" for name in tracked if name.endswith("/go.mod")]
    nested.append("third_party/go-quickjs/")
    names = sorted(name for name in names
                   if not name.startswith(tuple(nested + ["_refs/", ".git/", ".zed/"]))
                   and name not in {"noe", "agent-server-build.jsonl"}
                   and (ROOT / name).is_file())
    manifest = []
    proxy = output / "proxy" / MODULE / "@v"
    proxy.mkdir(parents=True)
    with zipfile.ZipFile(proxy / (VERSION + ".zip"), "w", zipfile.ZIP_DEFLATED) as archive:
        for name in names:
            data = (ROOT / name).read_bytes()
            manifest.append(dict(path=name, sha256=hashlib.sha256(data).hexdigest()))
            archive.writestr(f"{MODULE}@{VERSION}/{name}", data)
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    shutil.copyfile(ROOT / "go.mod", proxy / (VERSION + ".mod"))
    (proxy / (VERSION + ".info")).write_text(json.dumps(dict(Version=VERSION, Time="2026-10-19T00:00:00Z")))
    (proxy / "list").write_text(VERSION + "\n")

    # The engine candidate is an independent module. Downstream modules ignore
    # the NOE checkout's replace directives, so provide the exact dependency as
    # an artifact too, rather than relying on a pre-populated developer cache.
    engine_source = ROOT / "third_party/go-quickjs"
    private_modules = MODULE
    if engine_source.is_dir():
        engine_proxy = output / "proxy" / ENGINE / "@v"
        engine_proxy.mkdir(parents=True)
        engine_manifest = []
        engine_nested = [p.parent for p in engine_source.rglob("go.mod") if p.parent != engine_source]
        with zipfile.ZipFile(engine_proxy / (ENGINE_VERSION + ".zip"), "w", zipfile.ZIP_DEFLATED) as archive:
            for source in sorted(engine_source.rglob("*")):
                if not source.is_file() or ".git" in source.relative_to(engine_source).parts:
                    continue
                if any(folder == source.parent or folder in source.parents for folder in engine_nested):
                    continue
                name = source.relative_to(engine_source).as_posix()
                data = source.read_bytes()
                engine_manifest.append(dict(path=name, sha256=hashlib.sha256(data).hexdigest()))
                archive.writestr(f"{ENGINE}@{ENGINE_VERSION}/{name}", data)
        shutil.copyfile(engine_source / "go.mod", engine_proxy / (ENGINE_VERSION + ".mod"))
        (engine_proxy / (ENGINE_VERSION + ".info")).write_text(json.dumps(dict(Version=ENGINE_VERSION, Time="2026-10-23T00:00:00Z")))
        (engine_proxy / "list").write_text(ENGINE_VERSION + "\n")
        (output / "engine-manifest.json").write_text(json.dumps(engine_manifest, indent=2) + "\n")
        private_modules += "," + ENGINE

    local = consumer("local-consumer", True)
    command("local-tidy", ["go", "mod", "tidy"], local)
    command("local-imports", ["go", "list", "-f", "{{join .Imports \"\\n\"}}", "."], local)
    imports = (output / "local-imports.log").read_text().splitlines()
    assert MODULE + "/" + package in imports
    assert all("/internal/" not in name and not name.startswith("noe/") for name in imports)
    command("local-build", ["go", "build", "-o", str(local / "consumer"), "."], local)
    for index, mode in enumerate(modes):
        command(f"local-run-{index}", [str(local / "consumer"), *mode], local)
    command("local-versions", ["go", "list", "-m", MODULE, "github.com/go-quickjs/go-quickjs"], local)
    command("package-deps", ["go", "list", "-deps", MODULE + "/" + package], local)
    deps = (output / "package-deps.log").read_text().splitlines()
    forbidden_names = ["cli", "runtime"] if args.agent else ["cli", "runtime", "agent", "agentbridge", "credentials"]
    forbidden = [MODULE + "/internal/" + name for name in forbidden_names]
    assert not any(name == prefix or name.startswith(prefix + "/") for name in deps for prefix in forbidden)
    if not args.agent:
        assert not any(name.startswith(("github.com/openai/", "github.com/OpenRouterTeam/")) for name in deps)
    else:
        command("stdlib-deps", ["go", "list", "-deps", MODULE + "/stdlib"], local)
        generic_deps = (output / "stdlib-deps.log").read_text().splitlines()
        assert not any(name.startswith((MODULE + "/agent", MODULE + "/internal/agent", "github.com/openai/", "github.com/OpenRouterTeam/")) for name in generic_deps)

    artifact = consumer("artifact-consumer", False)
    cache = output / "modcache"
    artifact_env = dict(env, GOMODCACHE=str(cache), GOPRIVATE="", GONOPROXY="", GONOSUMDB=private_modules, GOSUMDB="sum.golang.org",
                        GOPROXY=",".join([(output / "proxy").as_uri(), (Path(host["GOMODCACHE"]) / "cache/download").as_uri(), "https://proxy.golang.org", "direct"]))
    command("artifact-tidy", ["go", "mod", "tidy"], artifact, artifact_env)
    assert "replace" not in (artifact / "go.mod").read_text()
    command("artifact-versions", ["go", "list", "-m", "-json", MODULE, "github.com/go-quickjs/go-quickjs"], artifact, artifact_env)
    decoder = json.JSONDecoder()
    versions_text = (output / "artifact-versions.log").read_text().strip()
    selected_modules = {}
    while versions_text:
        selected, end = decoder.raw_decode(versions_text)
        selected_modules[selected["Path"]] = selected
        versions_text = versions_text[end:].lstrip()
    if engine_source.is_dir():
        selected_engine = selected_modules[ENGINE]
        assert selected_engine["Version"] == ENGINE_VERSION, selected_engine
        assert "Replace" not in selected_engine, selected_engine
    command("artifact-build", ["go", "build", "-o", str(artifact / "consumer"), "."], artifact, artifact_env)
    for index, mode in enumerate(modes):
        command(f"artifact-run-{index}", [str(artifact / "consumer"), *mode], artifact, artifact_env)
    if args.cross:
        for target_os in ["linux", "darwin", "windows"]:
            for arch in ["amd64", "arm64"]:
                settings = dict(artifact_env, GOOS=target_os, GOARCH=arch, CGO_ENABLED="0")
                command(f"consumer-{target_os}-{arch}", ["go", "build", "-o", str(output / f"consumer-{target_os}-{arch}"), "."], artifact, settings)
    print(json.dumps(dict(status="passed", publication=False, module=MODULE, package=package, version=VERSION, files=len(manifest))), flush=True)


if __name__ == "__main__":
    main()
