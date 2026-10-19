package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNoticePreferencesDefaultsAndRestart(t *testing.T) {
	dir := t.TempDir()
	save := filepath.Join(dir, "receive")
	n, e := NewNode(filepath.Join(dir, "state"), "设置测试", save)
	if e != nil {
		t.Fatal(e)
	}
	if n.noticeSettings() != defaultNoticeSettings() {
		t.Fatal("legacy config lost enabled defaults")
	}
	if e = n.SetNoticeSettings(NoticeSettings{}); e != nil {
		t.Fatal(e)
	}
	n.Close()
	n, e = NewNode(filepath.Join(dir, "state"), "", "")
	if e != nil {
		t.Fatal(e)
	}
	if n.noticeSettings() != (NoticeSettings{}) {
		t.Fatal("all-off preferences were reset after restart")
	}
	s := n.noticeSettings()
	s.Sound = true
	s.InApp = true
	if e = n.SetNoticeSettings(s); e != nil {
		t.Fatal(e)
	}
	n.Close()
	n, e = NewNode(filepath.Join(dir, "state"), "", "")
	if e != nil {
		t.Fatal(e)
	}
	defer n.Close()
	if n.noticeSettings() != s {
		t.Fatal("independent switches changed together")
	}
	bytes, e := os.ReadFile(filepath.Join(dir, "state", "settings.json"))
	if e != nil || len(bytes) == 0 {
		t.Fatal("settings were not persisted")
	}
	// Muting effects must not lose messages or replay old alerts on re-enable.
	v := incomingNotices{}
	v.update(nil, "self", "")
	message := Task{ID: "muted", Direction: "in", Sender: "peer", Target: "self", Kind: "text", Status: "done"}
	if len(v.update([]Task{message}, "self", "")) != 1 || v.count("") != 1 {
		t.Fatal("muting lost the incoming message")
	}
	if len(v.update([]Task{message}, "self", "")) != 0 {
		t.Fatal("existing arrival would replay after re-enable")
	}
}
