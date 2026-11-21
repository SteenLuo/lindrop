package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

func browser(t *testing.T, n *Node, name string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Transport: &http.Transport{Proxy: nil}}
	body, _ := json.Marshal(map[string]string{"code": n.code, "name": name})
	r, e := c.Post("http://"+n.addr+"/api/pair", "application/json", bytes.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.Status)
	}
	return c
}

func TestPhoneUploadReceivesExplicitRefusal(t *testing.T) {
	n := isolatedTransferNode(t, "phone-entry")
	client := browser(t, n, "测试手机")
	base := "http://" + n.addr
	data := []byte("手机上传的文件内容")
	request, _ := json.Marshal(map[string]interface{}{"target": n.id, "offer": Offer{Title: "phone.bin", Files: []Meta{{Rel: "phone.bin", Size: int64(len(data)), SHA: hashBytes(data)}}}})
	response, err := client.Post(base+"/api/task/offer", "application/json", bytes.NewReader(request))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		ID    string
		Reply TaskReply
	}
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || result.Reply.Status != "waiting" {
		t.Fatal("phone offer", response.Status, result)
	}
	if err = n.Accept(result.ID, false); err != nil {
		t.Fatal(err)
	}
	if err = n.TaskAction(result.ID, "reject"); err != nil {
		t.Fatal(err)
	}
	upload, err := http.NewRequest("POST", base+"/api/task/chunk?id="+result.ID+"&index=0&offset=0", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	upload.Header.Set("X-LinNode-Upload", "1")
	upload.Header.Set("Content-Type", "application/octet-stream")
	response, err = client.Do(upload)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 400 || !bytes.Contains(body, []byte("对方已拒绝接收")) {
		t.Fatal("phone refusal feedback", response.Status, string(body))
	}
	n.mu.Lock()
	v := *n.cfg.Tasks[result.ID]
	n.mu.Unlock()
	if v.Status != "rejected" || v.Error != "" {
		t.Fatal("phone upload overwrote receiver refusal", v.Status, v.Error)
	}
}
func TestWebAuthority(t *testing.T) {
	n := testNode(t, "入口")
	base := "http://" + n.addr
	first, second := browser(t, n, "甲"), browser(t, n, "乙")
	r, e := first.Post(base+"/api/admin/name", "application/json", strings.NewReader(`{"Name":"bad"}`))
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("guest accessed admin")
	}
	offer := Offer{ID: rid(16), Title: "手机.txt", Files: []Meta{{Rel: "手机.txt", Size: 1, SHA: hashBytes([]byte("x"))}}}
	raw, _ := json.Marshal(map[string]interface{}{"target": n.id, "offer": offer})
	r, e = first.Post(base+"/api/task/offer", "application/json", bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.Status)
	}
	r, e = second.Post(base+"/api/task/offer", "application/json", bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("different phone accessed task")
	}
	req, _ := http.NewRequest("POST", base+"/api/pair", strings.NewReader(`{}`))
	req.Header.Set("Origin", "http://external.invalid")
	req.Header.Set("Content-Type", "application/json")
	r, e = first.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("cross-origin allowed")
	}
	offer.ID = strings.Repeat("../", 10) + "aa"
	if _, e = n.offer(n.id, n.id, offer); e == nil {
		t.Fatal("task ID traversal allowed")
	}
}
func TestCorruptBlockAndNoOverwrite(t *testing.T) {
	a, b := testNode(t, "A"), testNode(t, "B")
	connect(t, a, b)
	for i := 0; i < 2; i++ {
		offer := Offer{ID: rid(16), Sender: a.id, SenderName: "A", Title: "同名.txt", Files: []Meta{{Rel: "同名.txt", Size: 1, SHA: hashBytes([]byte("x"))}}}
		var reply TaskReply
		if e := a.Call(b.id, "offer", offer, &reply); e != nil {
			t.Fatal(e)
		}
		if e := b.Accept(offer.ID, false); e != nil {
			t.Fatal(e)
		}
		var ack struct{ Offset int64 }
		if e := a.Call(b.id, "chunk", Chunk{offer.ID, 0, 0, []byte("y")}, &ack); e != nil {
			t.Fatal(e)
		}
		if e := a.Call(b.id, "finish", struct{ ID string }{offer.ID}, &reply); e == nil {
			t.Fatal("corrupt block accepted")
		}
		if e := a.Call(b.id, "offer", offer, &reply); e != nil || reply.Offsets[0] != 0 {
			t.Fatal("bad data did not reset progress", e)
		}
		if e := a.Call(b.id, "chunk", Chunk{offer.ID, 0, 0, []byte("x")}, &ack); e != nil {
			t.Fatal(e)
		}
		if e := a.Call(b.id, "finish", struct{ ID string }{offer.ID}, &reply); e != nil {
			t.Fatal(e)
		}
	}
	b.mu.Lock()
	names := map[string]bool{}
	for _, task := range b.cfg.Tasks {
		names[task.Files[0].Final] = true
	}
	b.mu.Unlock()
	if len(names) != 2 {
		t.Fatal("duplicate overwrote original")
	}
}
