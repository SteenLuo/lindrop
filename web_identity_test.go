package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"reflect"
	"testing"
)

func pairIdentityTest(t *testing.T, c *http.Client, n *Node, body map[string]interface{}) WebClient {
	t.Helper()
	raw, _ := json.Marshal(body)
	r, e := c.Post("http://"+n.addr+"/api/pair", "application/json", bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal("pair", r.Status)
	}
	var got WebClient
	if e = json.NewDecoder(r.Body).Decode(&got); e != nil {
		t.Fatal(e)
	}
	return got
}
func TestPhoneRescanPreservesIdentityAndName(t *testing.T) {
	n := testNode(t, "入口")
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	first := pairIdentityTest(t, c, n, map[string]interface{}{"join": n.webToken, "name": "我的手机"})
	renamed := pairIdentityTest(t, c, n, map[string]interface{}{"key": first.Key, "name": " 测试手机A ", "rename": true})
	if renamed.ID != first.ID || renamed.Name != "测试手机A" {
		t.Fatal("rename changed identity or failed")
	}
	// A legacy QR request omits the key and submits the generic name again.
	again := pairIdentityTest(t, c, n, map[string]interface{}{"join": n.webToken, "name": "我的手机"})
	if again.ID != first.ID || again.Key != first.Key || again.Name != "测试手机A" {
		t.Fatal("same-browser QR rescan reset name or identity")
	}
	// Cookie recovery also handles a missing/stale localStorage key.
	again = pairIdentityTest(t, c, n, map[string]interface{}{"key": "unknown", "join": n.webToken})
	if again.ID != first.ID {
		t.Fatal("cookie identity recovery failed")
	}
	n.mu.Lock()
	count := len(n.cfg.Clients)
	n.mu.Unlock()
	if count != 1 {
		t.Fatal("rescan created duplicate phone", count)
	}
	jar2, _ := cookiejar.New(nil)
	second := pairIdentityTest(t, &http.Client{Jar: jar2}, n, map[string]interface{}{"join": n.webToken, "name": "我的手机"})
	if second.ID == first.ID || second.Key == first.Key {
		t.Fatal("two phones merged")
	}
	other := testNode(t, "新入口")
	third := pairIdentityTest(t, &http.Client{}, other, map[string]interface{}{"join": other.webToken, "key": first.Key, "name": "我的手机"})
	if third.ID == first.ID || third.Name != "我的手机" {
		t.Fatal("foreign node adopted another entry credential")
	}
}
func TestSameNamedDevicesAndSameTimeTasksHaveStableOrder(t *testing.T) {
	n := testNode(t, "入口")
	for _, key := range []string{"a", "b", "c"} {
		n.mu.Lock()
		n.cfg.Clients[key] = WebClient{ID: key, Key: key, Name: "我的手机"}
		n.mu.Unlock()
		browser := &http.Client{}
		pairIdentityTest(t, browser, n, map[string]interface{}{"key": key})
	}
	n.mu.Lock()
	for _, id := range []string{"a", "b", "c"} {
		n.cfg.Tasks[id] = &Task{ID: id, Created: 123, CreatedNS: 123000000000}
	}
	n.mu.Unlock()
	var want []string
	for i := 0; i < 200; i++ {
		var got []string
		for _, d := range n.devices() {
			got = append(got, d.ID)
		}
		if i == 0 {
			want = got
		} else if !reflect.DeepEqual(got, want) {
			t.Fatal("equal phone names changed device order")
		}
		tasks := n.snapshot(true, WebClient{})["tasks"].([]Task)
		if len(tasks) != 3 || tasks[0].ID != "a" || tasks[1].ID != "b" || tasks[2].ID != "c" {
			t.Fatal("same-time history changed order")
		}
	}
}

func TestPhoneCookieIsolationAcrossEntryPorts(t *testing.T) {
	a, b := testNode(t, "入口A"), testNode(t, "入口B")
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	one := pairIdentityTest(t, browser, a, map[string]interface{}{"join": a.webToken, "name": "测试手机A"})
	two := pairIdentityTest(t, browser, b, map[string]interface{}{"join": b.webToken, "name": "另一入口的手机"})
	if one.ID == two.ID {
		t.Fatal("entries merged browser identities")
	}
	for _, sample := range []struct {
		node *Node
		want WebClient
	}{{a, one}, {b, two}} {
		r, e := browser.Get("http://" + sample.node.addr + "/api/state")
		if e != nil {
			t.Fatal(e)
		}
		var state struct {
			Client WebClient `json:"client"`
		}
		e = json.NewDecoder(r.Body).Decode(&state)
		r.Body.Close()
		if e != nil || r.StatusCode != 200 || state.Client.ID != sample.want.ID || state.Client.Name != sample.want.Name {
			t.Fatal("another entry replaced this node's browser cookie", r.StatusCode, e)
		}
	}
}
