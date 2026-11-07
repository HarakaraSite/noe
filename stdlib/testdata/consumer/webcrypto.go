package main

import (
	"context"
	"fmt"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func webCrypto(ctx context.Context) error {
	main, err := stdlib.Execute(ctx, stdlib.Source{
		Name: "[external WebCrypto consumer]", Virtual: true, Module: true, Code: `
import nodeCrypto, {webcrypto, subtle as nodeSubtle} from "node:crypto";
const check=(condition,label)=>{if(!condition)throw Error("WebCrypto consumer: "+label)};
const hex=value=>Array.from(new Uint8Array(value),byte=>byte.toString(16).padStart(2,"0")).join("");
if(typeof fetch!=="undefined"||await import("noe:fs").then(()=>true,()=>false))throw Error("crypto selected host capabilities");
if(webcrypto!==globalThis.crypto||nodeSubtle!==crypto.subtle||nodeCrypto.webcrypto!==crypto)throw Error("crypto identity");
const encoder=new TextEncoder(),message=encoder.encode("what do ya want for nothing?");
const key=await crypto.subtle.importKey("raw",encoder.encode("Jefe"),{name:"HMAC",hash:{name:"SHA-256"}},true,["sign","verify"]);
const signature=await crypto.subtle.sign("HMAC",key,message);
if(hex(signature)!=="5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"||!await crypto.subtle.verify("HMAC",key,signature,message))throw Error("HMAC vector");
const modified=new Uint8Array(signature);modified[0]^=1;
if(await crypto.subtle.verify("HMAC",key,modified,message))throw Error("modified signature accepted");
const password=await crypto.subtle.importKey("raw",encoder.encode("password"),"PBKDF2",false,["deriveBits"]);
const pbkdf2=await crypto.subtle.deriveBits({name:"PBKDF2",salt:encoder.encode("salt"),iterations:1,hash:"SHA-256"},password,256);
if(hex(pbkdf2)!=="120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b")throw Error("PBKDF2 vector");
const ikm=new Uint8Array(22).fill(11),salt=Uint8Array.from({length:13},(_,i)=>i),info=Uint8Array.from({length:10},(_,i)=>240+i);
const base=await crypto.subtle.importKey("raw",ikm,"HKDF",false,["deriveKey"]);
const derived=await crypto.subtle.deriveKey({name:"HKDF",hash:"SHA-256",salt,info},base,
  {name:"HMAC",hash:"SHA-256",length:256},true,["sign"]);
if(derived.algorithm.length!==256||!derived.extractable||derived.usages.join()!=="sign"||
  hex(await crypto.subtle.exportKey("raw",derived)).slice(0,16)!=="3cb25f25faacd57a")throw Error("HKDF deriveKey");
export default "main WebCrypto consumer passed";
`}, stdlib.Profile{})
	if err != nil || string(main.Data) != `"main WebCrypto consumer passed"` {
		return fmt.Errorf("main WebCrypto consumer result=%+v error=%s", main, stdlib.Diagnostic(err))
	}

	worker := `import {parentPort} from "node:worker_threads";
if(typeof fetch!=="undefined"||await import("noe:fs").then(()=>true,()=>false))throw Error("Worker crypto selected host capabilities");
const data=new TextEncoder().encode("abc");
const digest=await crypto.subtle.digest("SHA-256",data);
if(Array.from(new Uint8Array(digest),byte=>byte.toString(16).padStart(2,"0")).join("")!==
  "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")throw Error("Worker digest");
parentPort.postMessage("Worker WebCrypto consumer passed");parentPort.close();`
	workerCode := fmt.Sprintf(`
import {Worker} from "node:worker_threads";
const worker=new Worker(%q,{eval:true,type:"module"});
const result=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});
await worker.terminate();
if(result!=="Worker WebCrypto consumer passed")throw Error("Worker result");
export default "Worker WebCrypto consumer passed";
`, worker)
	workerResult, err := stdlib.Execute(ctx, stdlib.Source{
		Name: "[external WebCrypto Worker consumer]", Virtual: true, Module: true, Code: workerCode,
	}, stdlib.Profile{Modules: stdlib.Modules{Workers: &stdlib.WorkerOptions{Max: 1}}})
	if err != nil || string(workerResult.Data) != `"Worker WebCrypto consumer passed"` {
		return fmt.Errorf("Worker WebCrypto consumer result=%+v error=%s", workerResult, stdlib.Diagnostic(err))
	}
	return nil
}
