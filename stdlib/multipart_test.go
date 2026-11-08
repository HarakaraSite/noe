package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const multipartContract = `
const encoder=new TextEncoder();
function joined(...parts){
 const chunks=parts.map(part=>typeof part==="string"?encoder.encode(part):part),out=new Uint8Array(chunks.reduce((size,part)=>size+part.byteLength,0));
 let offset=0;for(const chunk of chunks){out.set(chunk,offset);offset+=chunk.byteLength;}return out;
}
const makers=[(body,headers)=>new Response(body,{headers}),(body,headers)=>new Request("https://example.test/",{method:"POST",body,headers})];
for(const make of makers) {
 for(const text of ["before--fixtureafter","before--fixture--after","one\r\nx--fixture\r\nthree","","\r\n","ends\r","東京\n雪"]) {
  const body="--fixture\r\nContent-Disposition: form-data; name=\"note\"\r\n\r\n"+text+"\r\n--fixture--\r\n";
  const form=await make(body,{"content-type":"multipart/form-data; boundary=fixture"}).formData();
  check(form.get("note")===text,"only delimiter lines split text parts: "+JSON.stringify(text));
 }
 const file=joined(new Uint8Array([0,255,13,10]),"binary before--fixtureafter",new Uint8Array([128,34,13,10]));
 for(const tail of ["--fixture--","--fixture-- \t\r\nepilogue --fixture text"]) {
  const wire=joined("preamble --fixture text\r\n--fixture \t\r\nContent-Disposition: form-data; name=\"tag\"\r\n\r\none\r\n--fixture\r\nContent-Disposition: form-data; name=\"upload\"; filename=\"sample.bin\"\r\nContent-Type: application/octet-stream\r\n\r\n",file,"\r\n--fixture\r\nContent-Disposition: form-data; name=\"tag\"\r\n\r\ntwo\r\n"+tail);
  const chunks=[];for(let at=0;at<wire.length;at+=7)chunks.push(wire.subarray(at,at+7));
  const form=await make(ReadableStream.from(chunks),{"content-type":"multipart/form-data; boundary=\"fixture\""}).formData(),upload=form.get("upload");
  check([...form.keys()].join()==="tag,upload,tag"&&form.getAll("tag").join()==="one,two","multipart part order and duplicate names");
  check(upload instanceof File&&upload.name==="sample.bin"&&upload.type==="application/octet-stream","file metadata");
  check((await upload.bytes()).join()===file.join(),"binary bytes and trailing CRLF retained exactly");
 }
 const original="one\ntwo\rthree\r\nfour\r\r\n東京",expected="one\r\ntwo\r\nthree\r\nfour\r\n\r\n東京";
 const bytes=joined(new Uint8Array([0,255]),"file\nbytes\rretain\r\n",new Uint8Array([128]));
 const form=new FormData();form.append("note",original);form.append("note","");form.append("upload",new File([bytes],"sample.bin",{type:"application/octet-stream"}));
 const response=make(form),copy=response.clone(),wire=await response.text();
 check(wire.includes(expected)&&!wire.includes("one\ntwo"),"outgoing text values normalize CR/LF once");
 const parsed=await copy.formData();
 check(parsed.getAll("note").join("|")===expected+"|","normalized and empty text fields roundtrip");
 check((await parsed.get("upload").bytes()).join()===bytes.join(),"outgoing file bytes are never normalized");
 check(form.get("note")===original&&(await form.get("upload").bytes()).join()===bytes.join(),"serialization leaves FormData inputs unchanged");
 check((await make(new FormData()).formData()).get("missing")===null,"empty form");
}
`

func TestMultipartBoundariesAndTextNewlines(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, multipartContract)
}
