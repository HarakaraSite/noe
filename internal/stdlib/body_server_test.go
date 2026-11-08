package stdlib

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServeBodyReaderLocksAndResponseReuse(t *testing.T) {
	rig := newReceiveRig(t, 0, `async request=>{
 if(request.url.endsWith("/locked")||request.url.endsWith("/cancelled")){
  const response=new Response("must not be sent");
  if(request.url.endsWith("/locked"))response.body.getReader();else await response.body.cancel();
  return response;
 }
 const reader=request.body.getReader();
 if(request.bodyUsed)throw Error("reader acquisition disturbed the incoming request");
 let refused=false;try{await request.json();}catch(error){refused=error instanceof TypeError;}
 if(!refused||request.bodyUsed)throw Error("locked helper read");
 reader.releaseLock();const value=await request.json();
 if(value.message!=="東京の雪"||!request.bodyUsed)throw Error("incoming JSON body");
 return Response.json(value);
 }`)
	client := &http.Client{Timeout: 4 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Post(rig.url+"/json", "application/json", strings.NewReader(`{"message":"東京の雪"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || string(body) != `{"message":"東京の雪"}` {
		t.Fatalf("incoming JSON status=%d body=%q err=%v", response.StatusCode, body, err)
	}
	for _, path := range []string{"/locked", "/cancelled"} {
		response, err := client.Get(rig.url + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 422 || string(body) != "handled" {
			t.Fatalf("%s status=%d body=%q err=%v", path, response.StatusCode, body, err)
		}
	}
}
