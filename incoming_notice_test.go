package main

import "testing"

func TestIncomingNoticesReadAndProgressDoNotRepeat(t *testing.T) {
	v := incomingNotices{}
	old := Task{ID: "old", Direction: "in", Sender: "phone-a", Target: "self", Kind: "text", Status: "done"}
	if len(v.update([]Task{old}, "self", "")) != 0 || v.count("") != 0 {
		t.Fatal("old history alerted at startup")
	}
	a := old
	a.ID = "new-a"
	b := old
	b.ID = "new-b"
	b.Sender = "phone-b"
	file := old
	file.ID = "file"
	file.Kind = ""
	file.Status = "receiving"
	if len(v.update([]Task{a, b, file, old}, "self", "phone-a")) != 2 || v.count("phone-a") != 0 || v.count("phone-b") != 1 {
		t.Fatal("active/offscreen conversation unread counts")
	}
	file.Status = "done"
	if len(v.update([]Task{a, b, file, old}, "self", "")) != 1 || v.count("") != 2 {
		t.Fatal("completed auto-received file did not alert")
	}
	if len(v.update([]Task{a, b, file, old}, "self", "")) != 0 {
		t.Fatal("progress refresh repeated notification")
	}
	v.update([]Task{a, b, file, old}, "self", "phone-b")
	if v.count("phone-b") != 0 || v.count("phone-a") != 1 {
		t.Fatal("opening conversation cleared the wrong peer")
	}
	out := Task{ID: "out", Direction: "out", Sender: "self", Target: "phone-b", Status: "done"}
	relay := Task{ID: "relay", Direction: "web", Sender: "phone-a", Target: "other", Status: "browser"}
	if len(v.update([]Task{a, b, file, out, relay}, "self", "")) != 0 {
		t.Fatal("outgoing/relay produced incoming alert")
	}
	waiting := old
	waiting.ID = "waiting"
	waiting.Kind = ""
	waiting.Status = "waiting"
	if len(v.update([]Task{waiting}, "self", "")) != 1 {
		t.Fatal("pending file offer not noticed")
	}
	waiting.Status = "done"
	if len(v.update([]Task{waiting}, "self", "")) != 0 {
		t.Fatal("accepted file alerted twice")
	}
}
