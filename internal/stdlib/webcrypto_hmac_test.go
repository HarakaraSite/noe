package stdlib_test

import (
	"bytes"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

const hmacKeyCases = `
async function checkHMACKeys() {
 const s=crypto.subtle, data=new TextEncoder().encode("HMAC generated and imported keys");
 const check=(condition,label)=>{if(!condition)throw Error(label)};
 const rejects=async(name,work)=>{const result=work();check(result instanceof Promise,"not a Promise");try{await result}catch(error){check(error.name===name,"expected "+name+", got "+error);return}throw Error("accepted "+name)};
 for(const [hash,bits,alg] of [["SHA-1",512,"HS1"],["SHA-256",512,"HS256"],["SHA-384",1024,"HS384"],["SHA-512",1024,"HS512"]]){
  const key=await s.generateKey({name:"hmac",hash},true,["verify","sign","verify"]);
  check(key instanceof CryptoKey&&key.type==="secret"&&key.algorithm.name==="HMAC"&&key.algorithm.hash.name===hash&&key.algorithm.length===bits,"generated metadata");
  check(key.usages.join() === "sign,verify","normalized usages");
  const raw=await s.exportKey("raw",key),jwk=await s.exportKey("jwk",key);
  check(raw.byteLength===bits/8&&jwk.kty==="oct"&&jwk.alg===alg&&jwk.ext&&jwk.key_ops.join()==="sign,verify"&&!jwk.k.includes("="),"JWK metadata");
  const imported=await s.importKey("jwk",jwk,{name:"HMAC",hash},false,["verify"]);
  const signature=await s.sign("HMAC",key,data);
  check(await s.verify("HMAC",imported,signature,data),"generated/JWK signature");
  const changed=new Uint8Array(signature);changed[0]^=1;
  check(!await s.verify("HMAC",imported,changed,data),"modified signature accepted");
  await rejects("InvalidAccessError",()=>s.sign("HMAC",imported,data));
  await rejects("InvalidAccessError",()=>s.exportKey("jwk",imported));
  const expected=jwk.k;
  key.algorithm.hash.name="SHA-1";key.algorithm.length=1;key.usages.length=0;
  jwk.k="AA";jwk.alg="HS1";jwk.key_ops.length=0;jwk.ext=false;
  const second=await s.exportKey("jwk",key);
  check(second.k===expected&&second.alg===alg&&second.ext&&second.key_ops.join()==="sign,verify","visible metadata changed authority");
  check(await s.verify("HMAC",imported,await s.sign("HMAC",key,data),data),"tampering changed algorithm");
 }
 for(const length of [1,7,8,9,17,257,524296]){
  const key=await s.generateKey({name:"HMAC",hash:"SHA-256",length},true,["sign"]);
  const raw=new Uint8Array(await s.exportKey("raw",key));
  check(key.algorithm.length===length&&raw.length===Math.ceil(length/8),"explicit length");
  if(length%8)check((raw[raw.length-1]&((1<<(8-length%8))-1))===0,"non-byte-aligned key tail");
 }
 const secret=await s.generateKey({name:"HMAC",hash:"SHA-256"},false,["verify"]);
 Object.defineProperty(secret,"extractable",{value:true});secret.usages.push("sign");
 await rejects("InvalidAccessError",()=>s.exportKey("raw",secret));
 await rejects("InvalidAccessError",()=>s.exportKey("jwk",secret));
 await rejects("InvalidAccessError",()=>s.sign("HMAC",secret,data));
 await rejects("OperationError",()=>s.generateKey({name:"HMAC",hash:"SHA-256",length:0},true,["sign"]));
 await rejects("OperationError",()=>s.generateKey({name:"HMAC",hash:"SHA-256",length:0},true,[]));
 await rejects("SyntaxError",()=>s.generateKey({name:"HMAC",hash:"SHA-256"},true,[]));
 await rejects("SyntaxError",()=>s.generateKey({name:"HMAC",hash:"SHA-256"},true,["encrypt"]));
 await rejects("TypeError",()=>s.generateKey({name:"HMAC",hash:"SHA-256"},true,["other"]));
 await rejects("TypeError",()=>s.generateKey({name:"HMAC"},true,["sign"]));
 for(const length of [-1,Infinity,NaN,2**32,1n])await rejects("TypeError",()=>s.generateKey({name:"HMAC",hash:"SHA-256",length},true,["sign"]));
 await rejects("NotSupportedError",()=>s.generateKey({name:"AES-GCM",length:256},true,["encrypt"]));
 await rejects("NotSupportedError",()=>s.generateKey({name:"HMAC",hash:"SHA256"},true,["sign"]));
 return "ok";
}
`

func TestWebCryptoHMACGenerationAndJWKMainWorker(t *testing.T) {
	worker := `import {parentPort} from "node:worker_threads";` + hmacKeyCases + `
checkHMACKeys().then(value=>{parentPort.postMessage(value);parentPort.close()},error=>{parentPort.postMessage(String(error));parentPort.close()});`
	out, errOut := run(t, stdlib.Config{Workers: workerFiles(map[string]string{"./hmac-keys.mjs": worker})}, hmacKeyCases+`
checkHMACKeys().then(result=>{
 console.log("main",result);
 const worker=new require_worker_threads.Worker("./hmac-keys.mjs");
 worker.on("message",result=>console.log("worker",result));
}).catch(error=>console.log(error.stack));`)
	if out != "main ok\nworker ok" || errOut != "" {
		t.Fatalf("HMAC key results: stdout=%q stderr=%q", out, errOut)
	}
}

func TestWebCryptoHMACJWKValidationAndSnapshots(t *testing.T) {
	out, errOut := run(t, stdlib.Config{}, `(async()=>{
 const s=crypto.subtle,algorithm={name:"HMAC",hash:"SHA-256"};
 const rejects=async(name,work)=>{const promise=work();if(!(promise instanceof Promise))throw Error("not a Promise");try{await promise}catch(error){if(error.name===name)return;throw error}throw Error("accepted "+name)};
 for(const jwk of [{},{kty:"RSA",k:"AQID"},{kty:"oct"},{kty:"oct",k:""},
  {kty:"oct",k:"A"},{kty:"oct",k:"AQ=="},{kty:"oct",k:"AQ\n"},{kty:"oct",k:"AQ+_"},{kty:"oct",k:"é"},
  {kty:"oct",k:"AQID",alg:"HS384"},{kty:"oct",k:"AQID",alg:"hs256"},
  {kty:"oct",k:"AQID",use:"enc"},{kty:"oct",k:"AQID",key_ops:["verify"]},
  {kty:"oct",k:"AQID",key_ops:["sign","sign"]},{kty:"oct",k:"AQID",key_ops:["unknown","unknown","sign"]},
  {kty:"oct",k:"AQID",key_ops:[]},{kty:"oct",k:"AQID",ext:false}]){
  await rejects("DataError",()=>s.importKey("jwk",jwk,algorithm,true,["sign"]));
 }
 for(const jwk of [1,"jwk",{kty:Symbol(),k:"AQID"},{kty:"oct",k:Symbol()},
  {kty:"oct",k:"AQID",key_ops:"sign"},{kty:"oct",k:"AQID",key_ops:null},
  {kty:"oct",k:"AQID",key_ops:[Symbol()]},{kty:"oct",k:"AQID",oth:1}]){
  await rejects("TypeError",()=>s.importKey("jwk",jwk,algorithm,true,["sign"]));
 }
 for(const bytes of [new ArrayBuffer(3),new Uint8Array(3),new DataView(new ArrayBuffer(3)),new SharedArrayBuffer(3)]){
  await rejects("TypeError",()=>s.importKey("jwk",bytes,algorithm,true,["sign"]));
  bytes.kty="oct";bytes.k="AQID";
  await rejects("TypeError",()=>s.importKey("jwk",bytes,algorithm,true,["sign"]));
 }
 for(const bytes of [new ArrayBuffer(3),new Uint8Array(3),new DataView(new ArrayBuffer(3))]){
  let reads=0;
  await rejects("NotSupportedError",()=>s.importKey("jwk",bytes,{get name(){reads++;return "unknown"}},true,["sign"]));
  if(reads!==1)throw Error("algorithm normalization did not precede format rejection");
  await rejects("TypeError",()=>s.importKey("jwk",bytes,{name:"HMAC",hash:"SHA-256",length:0},true,["encrypt"]));
 }
 await rejects("DataError",()=>s.importKey("jwk",null,algorithm,true,["sign"]));
 await rejects("DataError",()=>s.importKey("raw",new Uint8Array([1]),{name:"HMAC",hash:"SHA-256",length:0},true,["encrypt"]));
 await rejects("SyntaxError",()=>s.importKey("jwk",{kty:"oct",k:"AQID"},algorithm,true,[]));
 await rejects("NotSupportedError",()=>s.importKey("jwk",{kty:"oct",k:"AQID"},"HKDF",false,["deriveBits"]));
 const key=await s.importKey("jwk",{kty:"oct",k:"__8",alg:"HS256",key_ops:["sign","unknown","encrypt"],ext:0},
  {name:"HMAC",hash:"SHA-256",length:9},false,["sign"]);
 if(key.algorithm.length!==9||key.extractable||key.usages.join()!=="sign")throw Error("imported policy");
 const truncated=await s.importKey("jwk",{kty:"oct",k:"__8"},{name:"HMAC",hash:"SHA-256",length:9},true,["sign"]);
 if((await s.exportKey("jwk",truncated)).k!=="_4A")throw Error("import bit mask");
 const original={kty:"oct",k:"AQID",alg:"HS256",key_ops:["sign"],ext:true};
 const pending=s.importKey("jwk",original,{name:"HMAC",get hash(){original.k="AA";original.key_ops[0]="verify";original.ext=false;return "SHA-256"}},true,["sign"]);
 const imported=await pending;original.k="BAUG";
 const exported=await s.exportKey("jwk",imported);
 if(exported.k!=="AQID"||exported.key_ops.join()!=="sign"||!exported.ext)throw Error("JWK was not snapshotted");
 const converted=await s.importKey("jwk",{kty:{toString:()=>"oct"},k:123,key_ops:new Set(["sign"]),ext:{}},algorithm,true,["sign"]);
 if((await s.exportKey("jwk",converted)).k!=="120")throw Error("dictionary conversions");
 const poison={};Object.defineProperty(poison,"k",{get(){throw poison}});
 if(await s.importKey("jwk",poison,algorithm,true,["sign"]).then(()=>null,error=>error)!==poison)throw Error("getter exception changed");
 console.log("ok");
})()`)
	if out != "ok" || errOut != "" {
		t.Fatalf("JWK validation output=%q stderr=%q", out, errOut)
	}
}

func TestWebCryptoHMACUsesBorrowedRandomnessAndReportsFailure(t *testing.T) {
	out, _ := run(t, stdlib.Config{Random: bytes.NewReader([]byte{255, 255, 3, 4})}, `(async()=>{
 const key=await crypto.subtle.generateKey({name:"HMAC",hash:"SHA-256",length:9},true,["sign"]);
 if((await crypto.subtle.exportKey("jwk",key)).k!=="_4A")throw Error("generation did not use host entropy");
 if(crypto.getRandomValues(new Uint8Array(2)).join()!=="3,4")throw Error("entropy order changed");
 console.log("ok");
})()`)
	if out != "ok" {
		t.Fatal(out)
	}
	out, _ = run(t, stdlib.Config{Random: bytes.NewReader(nil)}, `(async()=>{
 const failure=await crypto.subtle.generateKey({name:"HMAC",hash:"SHA-256"},true,["sign"]).then(()=>null,error=>error);
 if(!(failure instanceof DOMException)||failure.name!=="OperationError")throw Error("entropy failure");
 console.log("ok");
})()`)
	if out != "ok" {
		t.Fatal(out)
	}
}
