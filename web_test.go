package main

import (
	"bytes"
	"encoding/json"
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
