#!/usr/bin/env python3
"""Exercise unmodified jose Web Crypto paths in NOE, a Worker, and Node."""

import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
EXAMPLE = ROOT / "examples/crypto"
SDK = EXAMPLE / "node_modules/jose"
VERSION = "6.2.15"
INTEGRITY = "sha512-qdF6ewcZARtef63fJ9AKKZPgxw+t7USH6HLXYcD4CrdmSvDp79rpJSPFEKc90QBeGS63u1KQtN2MZQHC0yDIzA=="


def sdk_manifest():
    return [
        {"path": str(path.relative_to(SDK)), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
        for path in sorted((SDK / "dist/webapi").rglob("*.js"))
    ]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    binary, output = args.binary.resolve(), args.output.resolve()
    if output == ROOT or ROOT in output.parents:
        raise ValueError("--output must be outside the checkout")
    output.mkdir(parents=True, exist_ok=False)
    if not (SDK / "package.json").exists():
        raise RuntimeError("First run npm ci --ignore-scripts --no-audit --no-fund --prefix examples/crypto")
    assert json.loads((SDK / "package.json").read_text())["version"] == VERSION
    lock = json.loads((EXAMPLE / "package-lock.json").read_text())["packages"]["node_modules/jose"]
    assert lock["version"] == VERSION and lock["integrity"] == INTEGRITY
    before = sdk_manifest()
    (output / "sdk-manifest.json").write_text(json.dumps(before, indent=2) + "\n")
    records = []

    def command(name, arguments):
        start = time.monotonic()
        result = subprocess.run(arguments, cwd=ROOT, capture_output=True, text=True, timeout=30)
        (output / (name + ".stdout")).write_text(result.stdout)
        (output / (name + ".stderr")).write_text(result.stderr)
        records.append({"name": name, "command": arguments, "exit_code": result.returncode,
                        "seconds": round(time.monotonic() - start, 3)})
        (output / "results.json").write_text(json.dumps(records, indent=2) + "\n")
        assert result.returncode == 0 and not result.stderr, (name, result.stdout, result.stderr)
        return result.stdout

    def entry(name, peers=(), worker=False):
        path = output / (name + ".mjs")
        code = "import {runJoseCases} from " + json.dumps(str(EXAMPLE / "jose-cases.js")) + ";\n"
        code += "const report=await runJoseCases(" + json.dumps(list(peers)) + ");\n"
        if worker:
            code += 'import {parentPort} from "node:worker_threads";parentPort.postMessage(report);parentPort.close();\n'
        else:
            code += "console.log(JSON.stringify(report));\n"
        path.write_text(code)
        return path

    command("node-version", ["node", "--version"])
    command("noe-version", [str(binary), "--version"])
    node = json.loads(command("node-fixtures", ["node", str(entry("node-fixtures"))]))
    peers = node["tokens"]
    main_report = json.loads(command("noe-main", [str(binary), "run", "--no-inst", str(entry("noe-main", peers))]))
    worker = entry("worker", peers, worker=True)
    launcher = output / "worker-launcher.mjs"
    launcher.write_text('import {Worker} from "node:worker_threads";\n'
                        'const worker=new Worker(' + json.dumps(str(worker)) + ',{type:"module"});\n'
                        'const report=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});\n'
                        'await worker.terminate();console.log(JSON.stringify(report));\n')
    worker_report = json.loads(command("noe-worker", [str(binary), "run", "--no-inst", str(launcher)]))
    peer_report = json.loads(command("node-verifies-noe", ["node", str(entry("node-peer", main_report["tokens"]))]))
    assert main_report["tokens"] == peers == worker_report["tokens"] == peer_report["tokens"]
    assert main_report["peerTokens"] == worker_report["peerTokens"] == peer_report["peerTokens"] == len(peers)
    command("noe-contract-example", [str(binary), "run", "--no-inst", str(EXAMPLE / "webcrypto.js")])

    probe = output / "sdk-gaps.mjs"
    probe.write_text("import {generateSecret,exportJWK,CompactEncrypt} from " + json.dumps(str(SDK / "dist/webapi/index.js")) + ";\n" + r'''
const secret=new Uint8Array(32);secret.fill(1);
const key=await crypto.subtle.importKey("raw",secret,{name:"HMAC",hash:"SHA-256"},true,["sign"]);
const probes=[
 ["HMAC key generation",()=>generateSecret("HS256")],
 ["CryptoKey JWK export",()=>exportJWK(key)],
 ["AES-GCM JWE",()=>new CompactEncrypt(secret).setProtectedHeader({alg:"dir",enc:"A256GCM"}).encrypt(secret)],
];
const results=[];
for(const [operation,run] of probes){try{await run();results.push({operation,supported:true})}catch(error){results.push({operation,supported:false,error:error.name,message:error.message})}}
const members=["digest","importKey","exportKey","sign","verify","deriveBits","deriveKey","generateKey","encrypt","decrypt","wrapKey","unwrapKey"];
let cloneStatus;
try{const copied=structuredClone(key);cloneStatus=copied instanceof CryptoKey?"branded":"ordinary object"}catch(error){cloneStatus=error.name}
console.log(JSON.stringify({sdkPaths:results,subtleMethods:Object.fromEntries(members.map(name=>[name,typeof crypto.subtle[name]])),constructors:{Crypto:typeof globalThis.Crypto,SubtleCrypto:typeof globalThis.SubtleCrypto,CryptoKey:typeof globalThis.CryptoKey},cryptoKeyClone:cloneStatus}));
''')
    inventory = json.loads(command("sdk-gap-probes", [str(binary), "run", "--no-inst", str(probe)]))
    gaps = inventory["sdkPaths"]
    assert not any(item["supported"] for item in gaps), "Update the gap inventory for newly implemented capabilities"
    assert before == sdk_manifest(), "SDK sources changed during execution"
    summary = {"sdk": "jose", "version": VERSION, "integrity": INTEGRITY,
               "unmodified_sdk_files": len(before), "algorithms": main_report["algorithms"],
               "main": main_report["checks"], "worker": worker_report["checks"],
               "node_interoperability": True, "gaps": gaps, "noe_binary": str(binary),
               "standard_interface_inventory": inventory,
               "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest()}
    (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps(summary), flush=True)


if __name__ == "__main__":
    main()
