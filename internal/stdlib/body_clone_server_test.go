package stdlib

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServeRequestCloneBytesRemainIndependent(t *testing.T) {
	rig := newReceiveRig(t, 0, `async request=>{
 const copy=request.clone(),reader=request.body.getReader(),chunk=await reader.read();
 chunk.value.fill(90);reader.releaseLock();
 if(await copy.text()!=="native payload")throw Error("incoming clone was modified");
 return new Response("incoming clone isolated");
 }`)
	client := &http.Client{Timeout: 4 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Post(rig.url, "application/octet-stream", strings.NewReader("native payload"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || string(body) != "incoming clone isolated" {
		t.Fatalf("status=%d body=%q err=%v", response.StatusCode, body, err)
	}
}
