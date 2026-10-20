package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// No background discovery: protocol fixtures never advertise on a real LAN.
func isolatedTransferNode(t *testing.T, name string) *Node {
	t.Helper()
	n, err := NewNode(t.TempDir(), name, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(n.handler())
	n.addr = strings.TrimPrefix(s.URL, "http://")
	n.localAddr = n.addr
	t.Cleanup(s.Close)
	t.Cleanup(n.Close)
	return n
}

type shortBinaryWriter struct{ bytes.Buffer }

func (w *shortBinaryWriter) Write(p []byte) (int, error) {
	if len(p) > 7 {
		p = p[:7]
	}
	return w.Buffer.Write(p)
}

func TestBinaryFrameBoundsAndPartialIO(t *testing.T) {
	data := bytes.Repeat([]byte{0, 255, '"', '\\', '\n'}, 90000)
	f := Frame{Kind: "request", ID: "request", Request: &Envelope{Method: "chunk.binary", Binary: data, Data: json.RawMessage(`{"length":450000}`)}}
	var w shortBinaryWriter
	if err := writePacket(&w, f); err != nil {
		t.Fatal(err)
	}
	if w.Len() >= len(data)+binaryHeaderLimit {
		t.Fatal("payload was JSON encoded")
	}
	var got Frame
	if err := readPacket(bytes.NewReader(w.Bytes()), &got); err != nil || !bytes.Equal(got.Request.Binary, data) {
		t.Fatal("binary roundtrip", err)
	}
	for _, size := range []uint32{0, 5, 4 + binaryHeaderLimit + binaryChunkSize + 1} {
		if err := readBinaryPacket(bytes.NewReader(nil), size, &got); err == nil {
			t.Fatal("invalid total length accepted", size)
		}
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], binaryHeaderLimit+1)
	if err := readBinaryPacket(bytes.NewReader(prefix[:]), binaryHeaderLimit+20, &got); err == nil {
		t.Fatal("oversized header accepted")
	}
	if err := readPacket(bytes.NewReader(w.Bytes()[:w.Len()-1]), &got); err != io.ErrUnexpectedEOF {
		t.Fatal("truncated payload accepted", err)
	}
	f.Request.Binary = make([]byte, binaryChunkSize+1)
	if err := writePacket(io.Discard, f); err == nil {
		t.Fatal("oversized payload accepted")
	}
	var hello Hello
	if err := json.Unmarshal([]byte(`{"ID":"old","Name":"old"}`), &hello); err != nil || hello.BinaryChunks {
		t.Fatal("legacy Hello must disable binary")
	}
}

func TestDirectBinaryTransferAndLegacyFallback(t *testing.T) {
	for _, fast := range []bool{true, false} {
		t.Run(map[bool]string{true: "negotiated", false: "legacy"}[fast], func(t *testing.T) {
			a, b := isolatedTransferNode(t, "A"), isolatedTransferNode(t, "B")
			connect(t, a, b)
			if a.binaryLink(b.id) == nil || b.binaryLink(a.id) == nil {
				t.Fatal("capability not negotiated")
			}
			if !fast {
				a.mu.Lock()
				a.links[b.id].binaryChunks = false
				a.mu.Unlock()
			}
			b.mu.Lock()
			b.cfg.Trust[a.id] = true
			b.mu.Unlock()
			data := bytes.Repeat([]byte("完整性😀"), 900000)
			path := filepath.Join(t.TempDir(), "sample.bin")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			id, err := a.Send([]string{path}, b.id)
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, func() bool { return taskDone(a, id) && taskDone(b, id) })
			b.mu.Lock()
			dest := b.cfg.Tasks[id].Files[0].Final
			b.mu.Unlock()
			if got, err := shaFile(dest); err != nil || got != hashBytes(data) {
				t.Fatal("completed data mismatch", err)
			}
		})
	}
}

func TestBinaryChunkAuthenticityOwnershipAndResume(t *testing.T) {
	a, b, c := isolatedTransferNode(t, "A"), isolatedTransferNode(t, "B"), isolatedTransferNode(t, "C")
	connect(t, a, b)
	connect(t, c, b)
	data := bytes.Repeat([]byte("authenticated"), 100000)
	id := rid(16)
	offer := Offer{ID: id, Sender: a.id, Title: "data.bin", Files: []Meta{{Rel: "data.bin", Size: int64(len(data)), SHA: hashBytes(data)}}}
	var reply TaskReply
	if err := a.Call(b.id, "offer", offer, &reply); err != nil {
		t.Fatal(err)
	}
	if err := b.Accept(id, false); err != nil {
		t.Fatal(err)
	}
	makeEnv := func(origin *Node, payload []byte, offset int64) *Envelope {
		raw, _ := json.Marshal(binaryChunkHeader{id, 0, offset, len(payload), hashBytes(payload)})
		e := &Envelope{ID: rid(16), Origin: origin.id, Pub: origin.pub, Time: time.Now().Unix(), Target: b.id, Method: "chunk.binary", Data: raw, Binary: payload, Visited: []string{origin.id}}
		e.Signature, _ = ecdsa.SignASN1(rand.Reader, origin.key, signatureBody(e))
		return e
	}
	e := makeEnv(a, []byte("good"), 0)
	e.Binary = []byte("evil")
	if _, err := a.binaryLink(b.id).call(e); err == nil {
		t.Fatal("tampered binary accepted")
	}
	e = makeEnv(a, []byte("good"), 0)
	e.Signature[0] ^= 1
	if _, err := a.binaryLink(b.id).call(e); err == nil {
		t.Fatal("bad signature accepted")
	}
	e = makeEnv(c, []byte("good"), 0)
	if _, err := c.binaryLink(b.id).call(e); err == nil {
		t.Fatal("foreign owner wrote task")
	}
	e = makeEnv(a, []byte("good"), 1)
	if _, err := a.binaryLink(b.id).call(e); err == nil {
		t.Fatal("out of order block accepted")
	}
	e = makeEnv(a, []byte("good"), 0)
	e.Origin = c.id
	if _, err := a.binaryLink(b.id).call(e); err == nil {
		t.Fatal("TLS peer impersonated another origin")
	}
	var ack struct{ Offset int64 }
	if err := a.sendBinaryChunk(b.id, Chunk{id, 0, 0, data[:binaryChunkSize]}, &ack); err != nil {
		t.Fatal(err)
	}
	if err := a.Call(b.id, "offer", offer, &reply); err != nil || reply.Offsets[0] != binaryChunkSize {
		t.Fatal("resume negotiation", reply, err)
	}
	if err := a.sendBinaryChunk(b.id, Chunk{id, 0, ack.Offset, data[ack.Offset:]}, &ack); err != nil {
		t.Fatal(err)
	}
	if err := a.Call(b.id, "finish", struct{ ID string }{id}, &reply); err != nil || reply.Status != "done" {
		t.Fatal("finish", reply, err)
	}
	// Binary envelopes may never silently be forwarded to an older relay.
	e = makeEnv(a, []byte("good"), 0)
	e.Target = c.id
	if _, err := a.forward(e); err == nil {
		t.Fatal("binary request was relayed")
	}
}

func TestTransferRateUsesElapsedWindow(t *testing.T) {
	start := time.Now()
	m := transferRate{Started: start}
	if _, ready := m.add(1024*1024, start.Add(100*time.Millisecond)); ready {
		t.Fatal("instant block rate published")
	}
	rate, ready := m.add(1024*1024, start.Add(500*time.Millisecond))
	if !ready || rate != 4*1024*1024 {
		t.Fatal("window rate", rate, ready)
	}
	if _, ready = m.add(1, start.Add(501*time.Millisecond)); ready {
		t.Fatal("window did not reset")
	}
}

func TestBinaryTransferResumesAfterReceiverRestart(t *testing.T) {
	a, b := isolatedTransferNode(t, "A"), isolatedTransferNode(t, "B")
	connect(t, a, b)
	data := bytes.Repeat([]byte("restart-binary"), 100000)
	id := rid(16)
	offer := Offer{ID: id, Sender: a.id, Title: "restart.bin", Files: []Meta{{Rel: "restart.bin", Size: int64(len(data)), SHA: hashBytes(data)}}}
	var reply TaskReply
	if err := a.Call(b.id, "offer", offer, &reply); err != nil {
		t.Fatal(err)
	}
	if err := b.Accept(id, false); err != nil {
		t.Fatal(err)
	}
	var ack struct{ Offset int64 }
	if err := a.sendBinaryChunk(b.id, Chunk{id, 0, 0, data[:binaryChunkSize]}, &ack); err != nil {
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
	if err := a.Call(again.id, "offer", offer, &reply); err != nil || reply.Offsets[0] != binaryChunkSize {
		t.Fatal("restart lost resume offset", reply, err)
	}
	if err := a.sendBinaryChunk(again.id, Chunk{id, 0, ack.Offset, data[ack.Offset:]}, &ack); err != nil {
		t.Fatal(err)
	}
	if err := a.Call(again.id, "finish", struct{ ID string }{id}, &reply); err != nil {
		t.Fatal(err)
	}
	again.mu.Lock()
	dest := again.cfg.Tasks[id].Files[0].Final
	again.mu.Unlock()
	if got, err := shaFile(dest); err != nil || got != hashBytes(data) {
		t.Fatal("restart file mismatch", err)
	}
}
