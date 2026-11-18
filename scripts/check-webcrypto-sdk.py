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

    def entry(name, peers=(), generated_peers=(), worker=False):
        path = output / (name + ".mjs")
        code = "import {runJoseCases} from " + json.dumps(str(EXAMPLE / "jose-cases.js")) + ";\n"
        code += "const report=await runJoseCases(" + json.dumps(list(peers)) + ","
        code += json.dumps(list(generated_peers)) + ");\n"
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
    generated_peers = node["generatedKeys"]
    main_report = json.loads(command("noe-main", [str(binary), "run", "--no-inst", str(entry("noe-main", peers, generated_peers))]))
    worker = entry("worker", peers, generated_peers, worker=True)
    launcher = output / "worker-launcher.mjs"
    launcher.write_text('import {Worker} from "node:worker_threads";\n'
                        'const worker=new Worker(' + json.dumps(str(worker)) + ',{type:"module"});\n'
                        'const report=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});\n'
                        'await worker.terminate();console.log(JSON.stringify(report));\n')
    worker_report = json.loads(command("noe-worker", [str(binary), "run", "--no-inst", str(launcher)]))
    main_peer_report = json.loads(command(
        "node-verifies-noe-main",
        ["node", str(entry("node-main-peer", main_report["tokens"], main_report["generatedKeys"]))],
    ))
    worker_peer_report = json.loads(command(
        "node-verifies-noe-worker",
        ["node", str(entry("node-worker-peer", worker_report["tokens"], worker_report["generatedKeys"]))],
    ))

    def generated_token_count(report):
        return sum(len(group["tokens"]) for group in report["generatedKeys"])

    expected_generated_checks = {
        "keys": len(node["algorithms"]),
        "jwkExports": len(node["algorithms"]),
        "joseJwkImports": len(node["algorithms"]),
        "subtleJwkImports": len(node["algorithms"]),
        "signedAndVerifiedTokens": len(node["algorithms"]) * 3,
    }
    for report in (node, main_report, worker_report):
        assert report["generatedChecks"] == expected_generated_checks
        assert len(report["generatedKeys"]) == len(node["algorithms"])
    assert main_report["tokens"] == peers == worker_report["tokens"]
    assert main_report["peerTokens"] == worker_report["peerTokens"] == len(peers)
    expected_node_generated_tokens = generated_token_count(node)
    assert main_report["peerGeneratedTokens"] == worker_report["peerGeneratedTokens"] == expected_node_generated_tokens
    assert main_peer_report["peerTokens"] == len(main_report["tokens"])
    assert worker_peer_report["peerTokens"] == len(worker_report["tokens"])
    assert main_peer_report["peerGeneratedTokens"] == generated_token_count(main_report)
    assert worker_peer_report["peerGeneratedTokens"] == generated_token_count(worker_report)
    command("noe-contract-example", [str(binary), "run", "--no-inst", str(EXAMPLE / "webcrypto.js")])

    probe = output / "sdk-gaps.mjs"
    probe.write_text("import {generateSecret,exportJWK,importJWK,CompactEncrypt} from " + json.dumps(str(SDK / "dist/webapi/index.js")) + ";\n" + r'''
const secret=new Uint8Array(32);secret.fill(1);
const probes=[
 ["HMAC key generation",()=>generateSecret("HS256",{extractable:true})],
 ["CryptoKey JWK export",async()=>exportJWK(await generateSecret("HS256",{extractable:true}))],
 ["CryptoKey JWK import",async()=>{const key=await generateSecret("HS256",{extractable:true});const jwk=await exportJWK(key);return crypto.subtle.importKey("jwk",jwk,{name:"HMAC",hash:"SHA-256"},true,["sign","verify"])}],
 ["jose importJWK",async()=>{const key=await generateSecret("HS256",{extractable:true});return importJWK(await exportJWK(key),"HS256")}],
 ["AES-GCM JWE",()=>new CompactEncrypt(secret).setProtectedHeader({alg:"dir",enc:"A256GCM"}).encrypt(secret)],
];
const results=[];
for(const [operation,run] of probes){try{await run();results.push({operation,supported:true})}catch(error){results.push({operation,supported:false,error:error.name,message:error.message})}}
const members=["digest","importKey","exportKey","sign","verify","deriveBits","deriveKey","generateKey","encrypt","decrypt","wrapKey","unwrapKey"];
const cloneKey=await crypto.subtle.generateKey({name:"HMAC",hash:"SHA-256",length:256},false,["sign","verify"]);
let cloneStatus;
try{const copied=structuredClone(cloneKey);cloneStatus=copied instanceof CryptoKey?"branded":"ordinary object"}catch(error){cloneStatus=error.name}
console.log(JSON.stringify({sdkPaths:results,subtleMethods:Object.fromEntries(members.map(name=>[name,typeof crypto.subtle[name]])),constructors:{Crypto:typeof globalThis.Crypto,SubtleCrypto:typeof globalThis.SubtleCrypto,CryptoKey:typeof globalThis.CryptoKey},cryptoKeyClone:cloneStatus}));
''')
    inventory = json.loads(command("sdk-gap-probes", [str(binary), "run", "--no-inst", str(probe)]))
    capability_probes = inventory["sdkPaths"]
    expected_capabilities = {
        "HMAC key generation": True,
        "CryptoKey JWK export": True,
        "CryptoKey JWK import": True,
        "jose importJWK": True,
        "AES-GCM JWE": False,
    }
    observed_capabilities = {item["operation"]: item["supported"] for item in capability_probes}
    assert observed_capabilities == expected_capabilities, ("Unexpected SDK capability probes", capability_probes)
    expected_methods = {
        "digest": "function",
        "importKey": "function",
        "exportKey": "function",
        "sign": "function",
        "verify": "function",
        "deriveBits": "function",
        "deriveKey": "function",
        "generateKey": "function",
        "encrypt": "undefined",
        "decrypt": "undefined",
        "wrapKey": "undefined",
        "unwrapKey": "undefined",
    }
    assert inventory["subtleMethods"] == expected_methods, inventory["subtleMethods"]
    assert inventory["constructors"] == {
        "Crypto": "undefined",
        "SubtleCrypto": "undefined",
        "CryptoKey": "function",
    }, inventory["constructors"]
    assert inventory["cryptoKeyClone"] == "DataCloneError", inventory["cryptoKeyClone"]
    assert before == sdk_manifest(), "SDK sources changed during execution"
    remaining_gaps = [item for item in capability_probes if not item["supported"]]
    summary = {"sdk": "jose", "version": VERSION, "integrity": INTEGRITY,
               "unmodified_sdk_files": len(before), "algorithms": main_report["algorithms"],
               "main": main_report["checks"], "worker": worker_report["checks"],
               "generated_key_checks": {"node": node["generatedChecks"],
                                        "main": main_report["generatedChecks"],
                                        "worker": worker_report["generatedChecks"]},
               "generated_peer_tokens": {"node_to_main": main_report["peerGeneratedTokens"],
                                         "node_to_worker": worker_report["peerGeneratedTokens"],
                                         "main_to_node": main_peer_report["peerGeneratedTokens"],
                                         "worker_to_node": worker_peer_report["peerGeneratedTokens"]},
               "node_interoperability": True,
               "node_interoperability_directions": {"node_to_main": True, "main_to_node": True,
                                                     "node_to_worker": True, "worker_to_node": True},
               "gaps": remaining_gaps, "capability_probes": capability_probes,
               "noe_binary": str(binary),
               "standard_interface_inventory": inventory,
               "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest()}
    (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps(summary), flush=True)


if __name__ == "__main__":
    main()
