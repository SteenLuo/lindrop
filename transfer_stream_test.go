package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStreamingOfferAppearsBeforeFullSourceHash(t *testing.T) {
	for _, streaming := range []bool{true, false} {
		t.Run(map[bool]string{true: "new", false: "previous-capability"}[streaming], func(t *testing.T) {
			a, b := isolatedTransferNode(t, "A"), isolatedTransferNode(t, "B")
			connect(t, a, b)
			if !a.canStreamHashes(b.id) {
				t.Fatal("streaming capability missing")
			}
			if !streaming {
				a.mu.Lock()
				a.links[b.id].streamHashes = false
				a.mu.Unlock()
			}
			path := filepath.Join(t.TempDir(), "await-confirmation.bin")
			if err := os.WriteFile(path, bytes.Repeat([]byte("sample"), 100000), 0600); err != nil {
				t.Fatal(err)
			}
			id, err := a.Send([]string{path}, b.id)
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, func() bool {
				a.mu.Lock()
				defer a.mu.Unlock()
				return !a.busy[id] && a.cfg.Tasks[id].Status == "waiting_remote"
			})
			a.mu.Lock()
			sha := a.cfg.Tasks[id].Files[0].SHA
			a.mu.Unlock()
			b.mu.Lock()
			incoming := b.cfg.Tasks[id]
			flag := incoming.HashAtFinish
			remoteSHA := incoming.Files[0].SHA
			status := incoming.Status
			b.mu.Unlock()
			if status != "waiting" || flag != streaming || (sha == "") != streaming || remoteSHA != sha {
				t.Fatal("offer preparation policy", streaming, status, flag, sha, remoteSHA)
			}
			if err := b.Accept(id, false); err != nil {
				t.Fatal(err)
			}
			a.kick()
			waitFor(t, func() bool { return taskDone(a, id) && taskDone(b, id) })
		})
	}
}

func TestStreamingMixedFilesAndEmptyFileIntegrity(t *testing.T) {
	a, b := isolatedTransferNode(t, "A"), isolatedTransferNode(t, "B")
	connect(t, a, b)
	b.mu.Lock()
	b.cfg.Trust[a.id] = true
	b.mu.Unlock()
	source := t.TempDir()
	contents := map[string][]byte{"empty.bin": {}, "中文😀.bin": bytes.Repeat([]byte("payload"), 350000), "small.txt": []byte("small\r\n")}
	for name, data := range contents {
		if err := os.WriteFile(filepath.Join(source, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	id, err := a.Send([]string{source}, b.id)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return taskDone(a, id) && taskDone(b, id) })
	b.mu.Lock()
	files := append([]Meta(nil), b.cfg.Tasks[id].Files...)
	b.mu.Unlock()
	for _, f := range files {
		if got, err := shaFile(f.Final); err != nil || got != hashBytes(contents[filepath.Base(f.Rel)]) || got != f.SHA {
			t.Fatal("mixed-file digest mismatch", f.Rel, err)
		}
	}
}

func deferredOffer(t *testing.T, a, b *Node, data []byte) Offer {
	t.Helper()
	v := Offer{HashAtFinish: true, ID: rid(16), Sender: a.id, Title: "deferred.bin", Files: []Meta{{Rel: "deferred.bin", Size: int64(len(data))}}}
	var reply TaskReply
	if err := a.Call(b.id, "offer", v, &reply); err != nil {
		t.Fatal(err)
	}
	if err := b.Accept(v.ID, false); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDeferredFinishRequiresOwnedMatchingDigest(t *testing.T) {
	a, b, c := isolatedTransferNode(t, "A"), isolatedTransferNode(t, "B"), isolatedTransferNode(t, "C")
	connect(t, a, b)
	connect(t, c, b)
	data := []byte("authenticated deferred contents")
	v := deferredOffer(t, a, b, data)
	var ack struct{ Offset int64 }
	if err := a.sendBinaryChunk(b.id, Chunk{v.ID, 0, 0, data}, &ack); err != nil {
		t.Fatal(err)
	}
	var reply TaskReply
	for _, req := range []FinishRequest{{ID: v.ID}, {ID: v.ID, Hashes: []string{"invalid"}}, {ID: v.ID, Hashes: []string{hashBytes(data), hashBytes(data)}}} {
		if err := a.Call(b.id, "finish", req, &reply); err == nil {
			t.Fatal("missing or malformed final hashes accepted", req)
		}
	}
	if err := c.Call(b.id, "finish", FinishRequest{v.ID, []string{hashBytes(data)}}, &reply); err == nil {
		t.Fatal("foreign owner completed deferred file")
	}
	if err := a.Call(b.id, "finish", FinishRequest{v.ID, []string{hashBytes([]byte("wrong"))}}, &reply); err == nil {
		t.Fatal("wrong full digest accepted")
	}
	b.mu.Lock()
	job := b.cfg.Tasks[v.ID]
	offset, status, sha, final := job.Files[0].Offset, job.Status, job.Files[0].SHA, job.Files[0].Final
	b.mu.Unlock()
	if offset != 0 || status != "receiving" || sha != "" || final != "" {
		t.Fatal("checksum failure was not recoverable", offset, status, sha, final)
	}
	if err := a.sendBinaryChunk(b.id, Chunk{v.ID, 0, 0, data}, &ack); err != nil {
		t.Fatal(err)
	}
	if err := a.Call(b.id, "finish", FinishRequest{v.ID, []string{hashBytes(data)}}, &reply); err != nil || reply.Status != "done" {
		t.Fatal("retry did not complete", reply, err)
	}
	// Pre-hashed legacy tasks must never accept replacement digests.
	legacy := Offer{ID: rid(16), Sender: a.id, Title: "legacy.bin", Files: []Meta{{Rel: "legacy.bin", Size: int64(len(data)), SHA: hashBytes(data)}}}
	if err := a.Call(b.id, "offer", legacy, &reply); err != nil {
		t.Fatal(err)
	}
	if err := b.Accept(legacy.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := a.sendBinaryChunk(b.id, Chunk{legacy.ID, 0, 0, data}, &ack); err != nil {
		t.Fatal(err)
	}
	if err := a.Call(b.id, "finish", FinishRequest{legacy.ID, []string{hashBytes([]byte("changed"))}}, &reply); err == nil {
		t.Fatal("legacy digest was replaced")
	}
}

func TestDeferredOfferRejectsUnnegotiatedAndRelayedRequests(t *testing.T) {
	a, b := isolatedTransferNode(t, "A"), isolatedTransferNode(t, "B")
	connect(t, a, b)
	v := Offer{HashAtFinish: true, ID: rid(16), Sender: a.id, Title: "test.bin", Files: []Meta{{Rel: "test.bin", Size: 1}}}
	raw, _ := json.Marshal(v)
	if _, err := b.handle(&Envelope{Origin: a.id, Target: b.id, Method: "offer", Data: raw, Visited: []string{a.id, "relay"}}); err == nil {
		t.Fatal("relayed deferred offer accepted")
	}
	b.mu.Lock()
	b.links[a.id].streamHashes = false
	b.mu.Unlock()
	var reply TaskReply
	if err := a.Call(b.id, "offer", v, &reply); err == nil {
		t.Fatal("unnegotiated deferred offer accepted")
	}
	if _, err := b.offer(b.id, b.id, v); err == nil {
		t.Fatal("web/self deferred offer accepted")
	}
}

func TestDeferredResumeAfterReceiverRestartAndCompatibilityPath(t *testing.T) {
	a, b := isolatedTransferNode(t, "A"), isolatedTransferNode(t, "B")
	connect(t, a, b)
	data := bytes.Repeat([]byte("resume-source"), 200000)
	v := deferredOffer(t, a, b, data)
	var ack struct{ Offset int64 }
	if err := a.sendBinaryChunk(b.id, Chunk{v.ID, 0, 0, data[:binaryChunkSize]}, &ack); err != nil {
		t.Fatal(err)
	}
	dir, save := b.dir, b.cfg.SaveDir
	b.Close()
	again, err := NewNode(dir, "", save)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(again.handler())
	again.addr = strings.TrimPrefix(s.URL, "http://")
	again.localAddr = again.addr
	t.Cleanup(s.Close)
	t.Cleanup(again.Close)
	connect(t, a, again)
	source := filepath.Join(t.TempDir(), v.Title)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	files, _, _, err := collect([]string{source})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.cfg.Tasks[v.ID] = &Task{ID: v.ID, Direction: "out", Owner: a.id, Sender: a.id, Target: again.id, Title: v.Title, Files: files, Status: "queued", Total: int64(len(data))}
	a.mu.Unlock()
	if err := a.sendTask(v.ID); err != nil {
		t.Fatal(err)
	}
	again.mu.Lock()
	dest := again.cfg.Tasks[v.ID].Files[0].Final
	again.mu.Unlock()
	if got, err := shaFile(dest); err != nil || got != hashBytes(data) {
		t.Fatal("resumed streamed SHA omitted prefix", err)
	}
	// The same unfinished task may later travel via a compatibility path.
	v = deferredOffer(t, a, again, data)
	if err := a.sendBinaryChunk(again.id, Chunk{v.ID, 0, 0, data[:binaryChunkSize]}, &ack); err != nil {
		t.Fatal(err)
	}
	files, _, _, _ = collect([]string{source})
	a.mu.Lock()
	a.cfg.Tasks[v.ID] = &Task{ID: v.ID, Direction: "out", Owner: a.id, Sender: a.id, Target: again.id, Title: v.Title, Files: files, Status: "queued", Total: int64(len(data))}
	a.links[again.id].streamHashes = false
	a.mu.Unlock()
	if err := a.sendTask(v.ID); err != nil {
		t.Fatal("compatibility resume", err)
	}
	if !taskDone(again, v.ID) {
		t.Fatal("deferred receiver could not complete via compatibility mode")
	}
}

func TestStreamingSourceChangeAndInterruptedVerifyRecovery(t *testing.T) {
	a, b := isolatedTransferNode(t, "A"), isolatedTransferNode(t, "B")
	connect(t, a, b)
	source := filepath.Join(t.TempDir(), "changed.bin")
	if err := os.WriteFile(source, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := a.Send([]string{source}, b.id)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.busy[id] })
	if err := b.Accept(id, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("changed-and-larger"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.sendTask(id); err == nil {
		t.Fatal("changed source was sent")
	}
	if taskDone(b, id) {
		t.Fatal("changed source reported completion")
	}
	b.mu.Lock()
	b.cfg.Tasks[id].Status = "verifying"
	b.mu.Unlock()
	if err := b.persist(); err != nil {
		t.Fatal(err)
	}
	b.Close()
	n, err := NewNode(b.dir, "", b.cfg.SaveDir)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	n.mu.Lock()
	status := n.cfg.Tasks[id].Status
	n.mu.Unlock()
	if status != "receiving" {
		t.Fatal("interrupted verification cannot resume", status)
	}
	// A cancelled prefix scan must not continue to read gigabytes on exit.
	a.mu.Lock()
	a.cfg.Tasks[id].Manual = true
	a.mu.Unlock()
	f, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	start := time.Now()
	if err := a.hashSourcePrefix(id, f, 10, sha256.New()); err == nil || time.Since(start) > time.Second {
		t.Fatal("paused prefix scan did not cancel", err)
	}
}
