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
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--cross", action="store_true", help="compile consumer for six release targets")
    args = parser.parse_args()
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
        for source in (ROOT / "stdlib/testdata/consumer").glob("*.go"):
            shutil.copyfile(source, folder / source.name)
        text = f"module example.com/noe-stdlib-consumer\n\ngo 1.27.2\n\nrequire (\n {MODULE} {VERSION}\n github.com/go-quickjs/go-quickjs v0.27.0\n)\n"
        if replacement:
            text += f"\nreplace {MODULE} => {json.dumps(str(ROOT))}\n"
        (folder / "go.mod").write_text(text)
        shutil.copyfile(ROOT / "go.sum", folder / "go.sum")
        return folder

    # Only explicit tracked source and known implementation additions enter the zip.
    tracked = subprocess.check_output(["git", "ls-files", "-z"], cwd=ROOT).decode().split("\0")
    names = {name for name in tracked if name}
    names.update(str(p.relative_to(ROOT)) for p in (ROOT / "stdlib").rglob("*.go") if p.is_file())
    names.update(["scripts/check-stdlib-consumer.py", "docs/go-embedding.md", "examples/embedding/go.mod.template"])
    nested = [str(Path(name).parent) + "/" for name in tracked if name.endswith("/go.mod")]
    names = sorted(name for name in names if not name.startswith(tuple(nested + ["_refs/", ".git/"])) and name not in {"noe", "agent-server-build.jsonl"})
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

    local = consumer("local-consumer", True)
    command("local-tidy", ["go", "mod", "tidy"], local)
    command("local-imports", ["go", "list", "-f", "{{join .Imports \"\\n\"}}", "."], local)
    imports = (output / "local-imports.log").read_text().splitlines()
    assert MODULE + "/stdlib" in imports
    assert all("/internal/" not in name and not name.startswith("noe/") for name in imports)
    command("local-portable", ["go", "run", ".", "portable"], local)
    command("local-reference-build", ["go", "build", "-o", str(local / "consumer"), "."], local)
    command("local-reference", [str(local / "consumer"), "reference"], local)
    command("local-versions", ["go", "list", "-m", MODULE, "github.com/go-quickjs/go-quickjs"], local)
    command("package-deps", ["go", "list", "-deps", MODULE + "/stdlib"], local)
    deps = (output / "package-deps.log").read_text().splitlines()
    forbidden = [MODULE + "/internal/" + name for name in ["cli", "runtime", "agent", "credentials"]]
    assert not any(name == prefix or name.startswith(prefix + "/") for name in deps for prefix in forbidden)
    assert not any(name.startswith(("github.com/openai/", "github.com/OpenRouterTeam/")) for name in deps)

    artifact = consumer("artifact-consumer", False)
    cache = output / "modcache"
    artifact_env = dict(env, GOMODCACHE=str(cache), GOPRIVATE="", GONOPROXY="", GONOSUMDB=MODULE, GOSUMDB="sum.golang.org",
                        GOPROXY=",".join([(output / "proxy").as_uri(), (Path(host["GOMODCACHE"]) / "cache/download").as_uri(), "https://proxy.golang.org", "direct"]))
    command("artifact-tidy", ["go", "mod", "tidy"], artifact, artifact_env)
    assert "replace" not in (artifact / "go.mod").read_text()
    command("artifact-versions", ["go", "list", "-m", "-json", MODULE, "github.com/go-quickjs/go-quickjs"], artifact, artifact_env)
    command("artifact-portable", ["go", "run", ".", "portable"], artifact, artifact_env)
    command("artifact-reference-build", ["go", "build", "-o", str(artifact / "consumer"), "."], artifact, artifact_env)
    command("artifact-reference", [str(artifact / "consumer"), "reference"], artifact, artifact_env)
    if args.cross:
        for target_os in ["linux", "darwin", "windows"]:
            for arch in ["amd64", "arm64"]:
                settings = dict(artifact_env, GOOS=target_os, GOARCH=arch, CGO_ENABLED="0")
                command(f"consumer-{target_os}-{arch}", ["go", "build", "-o", str(output / f"consumer-{target_os}-{arch}"), "."], artifact, settings)
    print(json.dumps(dict(status="passed", publication=False, module=MODULE, version=VERSION, files=len(manifest))), flush=True)


if __name__ == "__main__":
    main()
