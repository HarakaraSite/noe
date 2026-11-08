package stdlib

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServeResponsePreservesSeparateCookies(t *testing.T) {
	rig := newReceiveRig(t, 0, `request=>{
 if(request.url.endsWith("/error"))return Response.error();
 const response=new Response("cookies",{status:201,headers:[
 ["Set-Cookie","a=1; Path=/"],["Set-Cookie","b=2; Path=/"],["X-List","one"],["x-list","two"],["X-Byte",request.headers.get("x-byte")]]});
 Object.defineProperties(response,{status:{get(){throw Error("shadow status");}},headers:{get(){throw Error("shadow headers");}}});
 return response;
 }`)
	client := &http.Client{Timeout: 4 * time.Second}
	defer client.CloseIdleConnections()
	request, err := http.NewRequest("GET", rig.url, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Byte", "\xff\xc3\xa9")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 201 || string(body) != "cookies" {
		t.Fatalf("status=%d body=%q err=%v", response.StatusCode, body, err)
	}
	if got := strings.Join(response.Header.Values("Set-Cookie"), "|"); got != "a=1; Path=/|b=2; Path=/" {
		t.Fatalf("Set-Cookie fields=%q", got)
	}
	if got := response.Header.Get("X-List"); got != "one, two" {
		t.Fatalf("combined ordinary field=%q", got)
	}
	if got := response.Header.Get("X-Byte"); got != "\xff\xc3\xa9" {
		t.Fatalf("incoming/outgoing ByteString=%q", got)
	}
	invalid, err := client.Get(rig.url + "/error")
	if err != nil {
		t.Fatal(err)
	}
	defer invalid.Body.Close()
	if invalid.StatusCode != 422 {
		t.Fatalf("network-error Response did not reach onError: status=%d", invalid.StatusCode)
	}
}
