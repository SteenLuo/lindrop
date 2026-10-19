package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLoopbackStartupUsesOneListener(t *testing.T) {
	n, err := NewNode(t.TempDir(), "回环检查", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.Close)
	if err = n.Start("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if n.addr != n.localAddr || len(n.httpServers) != 1 {
		t.Fatal("loopback endpoint must be served once")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	r, err := client.Get("http://" + n.localAddr + "/")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("entry status %d", r.StatusCode)
	}
}

func testNode(t *testing.T, name string) *Node {
	t.Helper()
	n, e := NewNode(filepath.Join(t.TempDir(), "state"), name, filepath.Join(t.TempDir(), "receive"))
	if e != nil {
		t.Fatal(e)
	}
	if e = n.Start("127.0.0.2", 0); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(n.Close)
	return n
}
func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(70 * time.Millisecond)
	}
	t.Fatal("condition timeout")
}
func connect(t *testing.T, a, b *Node) {
	t.Helper()
	if e := a.Dial(b.addr, b.code); e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool {
		a.mu.Lock()
		_, ok := a.links[b.id]
		a.mu.Unlock()
		b.mu.Lock()
		_, ok2 := b.links[a.id]
		b.mu.Unlock()
		return ok && ok2
	})
	a.announce()
	b.announce()
}
func taskDone(n *Node, id string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	v := n.cfg.Tasks[id]
	return v != nil && v.Status == "done"
}
func TestPersistentIdentityAndName(t *testing.T) {
	dir := t.TempDir()
	save := t.TempDir()
	a, e := NewNode(dir, "书房电脑", save)
	if e != nil {
		t.Fatal(e)
	}
	id := a.id
	a.SetName("剪辑电脑")
	a.Close()
	b, e := NewNode(dir, "", "")
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if b.id != id || b.name() != "剪辑电脑" || b.cfg.SaveDir != save {
		t.Fatal("identity or settings lost")
	}
}
func TestOneConnectionBothDirections(t *testing.T) {
	a, b := testNode(t, "A"), testNode(t, "B")
	connect(t, a, b)
	a.mu.Lock()
	a.cfg.Trust[b.id] = true
	a.mu.Unlock()
	b.mu.Lock()
	b.cfg.Trust[a.id] = true
	b.mu.Unlock()
	path := filepath.Join(t.TempDir(), "中文素材.bin")
	data := bytes.Repeat([]byte("双向传输"), 250000)
	os.WriteFile(path, data, 0600)
	id, e := a.Send([]string{path}, b.id)
	if e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool { return taskDone(a, id) && taskDone(b, id) })
	id2, e := b.Send([]string{path}, a.id)
	if e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool { return taskDone(a, id2) && taskDone(b, id2) })
	for _, n := range []*Node{a, b} {
		n.mu.Lock()
		if len(n.links) != 1 {
			t.Fatal("expected one duplex link")
		}
		task := n.cfg.Tasks[id]
		if n == a {
			task = n.cfg.Tasks[id2]
		}
		dest := task.Files[0].Final
		n.mu.Unlock()
		got, e := os.ReadFile(dest)
		if e != nil || !bytes.Equal(got, data) {
			t.Fatal("content mismatch", e)
		}
	}
}
func TestRelayAndSharedDownload(t *testing.T) {
	a, b, c := testNode(t, "A"), testNode(t, "中转B"), testNode(t, "C")
	connect(t, a, b)
	connect(t, c, b)
	waitFor(t, func() bool {
		a.announce()
		b.announce()
		c.announce()
		for _, d := range a.devices() {
			if d.ID == c.id && d.Online {
				return true
			}
		}
		return false
	})
	c.mu.Lock()
	c.cfg.Trust[a.id] = true
	c.mu.Unlock()
	folder := filepath.Join(t.TempDir(), "项目素材")
	os.MkdirAll(filepath.Join(folder, "子目录"), 0700)
	data := bytes.Repeat([]byte("relay\x00"), 900000)
	os.WriteFile(filepath.Join(folder, "子目录", "视频.bin"), data, 0600)
	os.WriteFile(filepath.Join(folder, "空文件.txt"), nil, 0600)
	id, e := a.Send([]string{folder}, c.id)
	if e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool { return taskDone(a, id) && taskDone(c, id) })
	c.mu.Lock()
	job := c.cfg.Tasks[id]
	files := append([]Meta{}, job.Files...)
	c.mu.Unlock()
	for _, f := range files {
		got, e := os.ReadFile(f.Final)
		if e != nil || hashBytes(got) != f.SHA {
			t.Fatal("relay hash mismatch", e)
		}
	}
	b.mu.Lock()
	if len(b.cfg.Tasks) != 0 {
		t.Fatal("relay created a stored receiving task")
	}
	b.mu.Unlock()
	if e = c.AddShare([]string{folder}); e != nil {
		t.Fatal(e)
	}
	var shares []Share
	waitFor(t, func() bool {
		if a.Call(c.id, "shares", struct{}{}, &shares) != nil || len(shares) == 0 {
			return false
		}
		for _, f := range shares[0].Files {
			if len(f.SHA) != 64 {
				return false
			}
		}
		return true
	})
	pull, e := a.Pull(c.id, shares[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool { return taskDone(a, pull) })
	a.mu.Lock()
	if a.links[c.id] != nil {
		t.Fatal("relay test accidentally made direct link")
	}
	a.mu.Unlock()
}
func TestResumeAfterReceiverRestartAndPermissions(t *testing.T) {
	a, b := testNode(t, "A"), testNode(t, "B")
	connect(t, a, b)
	data := bytes.Repeat([]byte("resume"), 350000)
	offer := Offer{ID: rid(16), Sender: a.id, SenderName: "A", Title: "续传.bin", Files: []Meta{{Rel: "续传.bin", Size: int64(len(data)), SHA: hashBytes(data)}}}
	var reply TaskReply
	if e := a.Call(b.id, "offer", offer, &reply); e != nil {
		t.Fatal(e)
	}
	if reply.Status != "waiting" {
		t.Fatal("first receive must need confirmation")
	}
	if e := b.Accept(offer.ID, false); e != nil {
		t.Fatal(e)
	}
	var ack struct{ Offset int64 }
	if e := a.Call(b.id, "chunk", Chunk{offer.ID, 0, 0, data[:chunkSize]}, &ack); e != nil {
		t.Fatal(e)
	}
	var denied struct{}
	if e := b.Call(b.id, "chunk", Chunk{offer.ID, 0, chunkSize, []byte("bad")}, &denied); e == nil {
		t.Fatal("foreign owner wrote task")
	}
	dir, save, addr := b.dir, b.cfg.SaveDir, b.addr
	b.Close()
	_, p, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(p)
	again, e := NewNode(dir, "", save)
	if e != nil {
		t.Fatal(e)
	}
	if e = again.Start("127.0.0.2", port); e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	waitFor(t, func() bool { a.mu.Lock(); v := a.links[again.id]; a.mu.Unlock(); return v == nil })
	if e = a.Dial(again.addr, ""); e != nil {
		t.Fatal(e)
	}
	if e = a.Call(again.id, "offer", offer, &reply); e != nil {
		t.Fatal(e)
	}
	if reply.Offsets[0] != chunkSize {
		t.Fatal("progress not restored", reply)
	}
	for off := int64(chunkSize); off < int64(len(data)); {
		end := off + chunkSize
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		if e = a.Call(again.id, "chunk", Chunk{offer.ID, 0, off, data[off:end]}, &ack); e != nil {
			t.Fatal(e)
		}
		off = ack.Offset
	}
	if e = a.Call(again.id, "finish", struct{ ID string }{offer.ID}, &reply); e != nil {
		t.Fatal(e)
	}
	if reply.Status != "done" {
		t.Fatal(reply)
	}
	bad := offer
	bad.ID = rid(16)
	bad.Files = []Meta{{Rel: "../escape", Size: 1, SHA: hashBytes([]byte("x"))}}
	if e = a.Call(again.id, "offer", bad, &reply); e == nil {
		t.Fatal("accepted traversal")
	}
}
func TestDirectConnectAndLoop(t *testing.T) {
	a, b := testNode(t, "A"), testNode(t, "B")
	if e := a.Dial(b.addr, ""); e != nil {
		t.Fatal("fresh local nodes required pairing", e)
	}
	env := &Envelope{Visited: []string{a.id}}
	if _, e := a.forward(env); e == nil {
		t.Fatal("loop accepted")
	}
	bad := bytes.NewBuffer([]byte{0, 64, 0, 1})
	var v interface{}
	if readPacket(bad, &v) == nil {
		t.Fatal("oversize packet accepted")
	}
	raw, _ := json.Marshal(a.snapshot(true, WebClient{}))
	if len(raw) == 0 {
		t.Fatal("empty snapshot")
	}
}

func TestRelayRestartAutomaticallyContinues(t *testing.T) {
	a, b, c := testNode(t, "发送A"), testNode(t, "中转B"), testNode(t, "接收C")
	connect(t, a, b)
	connect(t, c, b)
	waitFor(t, func() bool {
		a.announce()
		b.announce()
		c.announce()
		for _, d := range a.devices() {
			if d.ID == c.id && d.Online {
				return true
			}
		}
		return false
	})
	c.mu.Lock()
	c.cfg.Trust[a.id] = true
	c.mu.Unlock()
	source := filepath.Join(t.TempDir(), "中转续传.bin")
	data := bytes.Repeat([]byte("r"), 64*1024*1024)
	expected := hashBytes(data)
	if e := os.WriteFile(source, data, 0600); e != nil {
		t.Fatal(e)
	}
	data = nil
	id, e := a.Send([]string{source}, c.id)
	if e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		job := c.cfg.Tasks[id]
		return job != nil && job.Done > 0
	})
	dir, save, addr := b.dir, b.cfg.SaveDir, b.addr
	b.Close()
	_, p, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(p)
	again, e := NewNode(dir, "", save)
	if e != nil {
		t.Fatal(e)
	}
	if e = again.Start("127.0.0.2", port); e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	waitFor(t, func() bool { return taskDone(a, id) && taskDone(c, id) })
	c.mu.Lock()
	dest := c.cfg.Tasks[id].Files[0].Final
	c.mu.Unlock()
	got, e := shaFile(dest)
	if e != nil || got != expected {
		t.Fatal("resumed relay file mismatch", e)
	}
}

func TestAddressReassignmentKeepsDistinctIdentity(t *testing.T) {
	a, b := testNode(t, "A"), testNode(t, "原设备B")
	connect(t, a, b)
	addr := b.addr
	b.Close()
	_, p, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(p)
	c, e := NewNode(t.TempDir(), "新设备C", t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Start("127.0.0.2", port); e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if e = a.dialExpected(c.addr, "", b.id); e == nil {
		t.Fatal("known reconnect accepted mismatched certificate")
	}
	if e = a.Dial(c.addr, ""); e != nil {
		t.Fatal("manual address could not connect new identity", e)
	}
	a.mu.Lock()
	old := a.cfg.Peers[b.id]
	a.mu.Unlock()
	if old.Addr != "" {
		t.Fatal("stale address binding retained")
	}
}
