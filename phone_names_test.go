package main

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

func TestLANPhoneNameReuseDoesNotShareIdentityOrSecrets(t *testing.T) {
	a, b := testNode(t, "书房电脑"), testNode(t, "另一台电脑")
	connect(t, a, b)
	jar, _ := cookiejar.New(nil)
	firstBrowser := &http.Client{Jar: jar}
	source := pairIdentityTest(t, firstBrowser, a, map[string]interface{}{"join": a.webToken, "name": "测试手机A"})
	jar2, _ := cookiejar.New(nil)
	nextBrowser := &http.Client{Jar: jar2}
	current := pairIdentityTest(t, nextBrowser, b, map[string]interface{}{"join": b.webToken, "name": "我的手机"})
	secondPhone := pairIdentityTest(t, &http.Client{}, b, map[string]interface{}{"join": b.webToken, "name": "测试手机B"})
	ref := "web:" + a.id + ":" + source.ID
	names := b.availablePhoneNames()
	found := false
	for _, p := range names {
		if p.Ref == ref && p.Name == "测试手机A" && p.Entry == "书房电脑" {
			found = true
		}
	}
	if !found {
		t.Fatal("LAN peer did not provide registered phone name", names)
	}
	raw, _ := json.Marshal(names)
	if strings.Contains(string(raw), source.Key) || strings.Contains(string(raw), current.Key) || strings.Contains(string(raw), "\"key\"") {
		t.Fatal("phone name suggestions disclosed browser credentials")
	}
	name, e := b.usePhoneName(current, ref)
	if e != nil || name != "测试手机A" {
		t.Fatal("could not reuse LAN name", e)
	}
	b.mu.Lock()
	actual := b.cfg.Clients[current.Key]
	other := b.cfg.Clients[secondPhone.Key]
	count := len(b.cfg.Clients)
	b.mu.Unlock()
	if actual.ID != current.ID || actual.Key != current.Key || actual.ID == source.ID || count != 2 {
		t.Fatal("name selection replaced/merged phone identity")
	}
	if other.Name != "测试手机B" {
		t.Fatal("selection renamed a different phone")
	}
	if _, e = b.usePhoneName(current, "web:unknown:unknown"); e == nil {
		t.Fatal("accepted an invented shared identity")
	}
	for _, path := range []string{"/api/phone-names", "/api/state"} {
		r, e := http.Get("http://" + b.addr + path)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 401 {
			t.Fatal("unauthenticated name registry access", r.StatusCode)
		}
	}
	response, e := nextBrowser.Post("http://"+b.addr+"/api/phone-name", "application/json", strings.NewReader(`{"ref":"`+ref+`"}`))
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("browser name reuse API", response.Status)
	}
	// The selected browser remains authenticated under its original local key.
	response, e = nextBrowser.Get("http://" + b.addr + "/api/state")
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	var state struct {
		Client WebClient `json:"client"`
	}
	json.NewDecoder(response.Body).Decode(&state)
	if state.Client.ID != current.ID || state.Client.Name != "测试手机A" {
		t.Fatal("name selection broke browser session")
	}
}
