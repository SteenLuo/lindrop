package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRejectIncomingWaitingReceivingAndPaused(t *testing.T) {
	for _, stage := range []string{"waiting", "receiving", "paused"} {
		t.Run(stage, func(t *testing.T) {
			a, b := isolatedTransferNode(t, "发送"), isolatedTransferNode(t, "接收")
			connect(t, a, b)
			data := bytes.Repeat([]byte("不能接收的文件"), 10000)
			source := filepath.Join(t.TempDir(), "来件.bin")
			if err := os.WriteFile(source, data, 0600); err != nil {
				t.Fatal(err)
			}
			id, err := a.Send([]string{source}, b.id)
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, func() bool {
				a.mu.Lock()
				defer a.mu.Unlock()
				return !a.busy[id] && a.cfg.Tasks[id].Status == "waiting_remote"
			})
			if stage != "waiting" {
				if err = b.Accept(id, false); err != nil {
					t.Fatal(err)
				}
				var ack ChunkReply
				if err = a.Call(b.id, "chunk", Chunk{id, 0, 0, data[:4096]}, &ack); err != nil || ack.Offset != 4096 {
					t.Fatal("initial chunk", ack, err)
				}
				if stage == "paused" {
					if err = b.TaskAction(id, "pause"); err != nil {
						t.Fatal(err)
					}
				}
			}
			b.mu.Lock()
			temp := b.cfg.Tasks[id].Files[0].Temp
			save := b.cfg.SaveDir
			b.mu.Unlock()
			keep := filepath.Join(save, "保留的已有文件.txt")
			if err = os.WriteFile(keep, []byte("必须保留"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = b.TaskAction(id, "reject"); err != nil {
				t.Fatal(err)
			}
			if temp != "" {
				if _, err = os.Stat(temp); !os.IsNotExist(err) {
					t.Fatal("rejected partial file remains", err)
				}
			}
			if actual, e := os.ReadFile(keep); e != nil || string(actual) != "必须保留" {
				t.Fatal("reject damaged existing files", e)
			}
			if actual, e := os.ReadFile(source); e != nil || !bytes.Equal(actual, data) {
				t.Fatal("reject changed sender source", e)
			}
			for _, binary := range []bool{false, true} {
				var ack ChunkReply
				if binary {
					err = a.sendBinaryChunk(b.id, Chunk{id, 0, 0, data[:2048]}, &ack)
				} else {
					err = a.Call(b.id, "chunk", Chunk{id, 0, 0, data[:2048]}, &ack)
				}
				if err != nil || ack.Status != "rejected" {
					t.Fatal("late chunk did not preserve rejection", binary, ack, err)
				}
			}
			var reply TaskReply
			if err = a.Call(b.id, "finish", FinishRequest{ID: id}, &reply); err != nil || reply.Status != "rejected" {
				t.Fatal("late finish revived rejected transfer", reply, err)
			}
			if err = a.sendTask(id); err != nil {
				t.Fatal("sender could not observe rejection", err)
			}
			a.mu.Lock()
			status := a.cfg.Tasks[id].Status
			a.mu.Unlock()
			if status != "rejected" {
				t.Fatal("sender did not stop", status)
			}
			if err = b.TaskAction(id, "resume"); err == nil {
				t.Fatal("rejected task could resume")
			}
			b.setTask(id, "paused", "late worker error")
			b.mu.Lock()
			status = b.cfg.Tasks[id].Status
			b.mu.Unlock()
			if status != "rejected" {
				t.Fatal("worker resurrected rejected task")
			}
			if temp != "" {
				if _, err = os.Stat(temp); !os.IsNotExist(err) {
					t.Fatal("late messages recreated partial file", err)
				}
			}
			b.Close()
			reopened, err := NewNode(b.dir, "", "")
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if reopened.cfg.Tasks[id].Status != "rejected" || reopened.TaskAction(id, "resume") == nil {
				t.Fatal("restart lost terminal rejection")
			}
		})
	}
}

type pacedTransferConn struct{ net.Conn }

func (c pacedTransferConn) Write(data []byte) (int, error) {
	total := 0
	for len(data) > 0 {
		size := len(data)
		if size > 32768 {
			size = 32768
		}
		time.Sleep(time.Millisecond)
		n, err := c.Conn.Write(data[:size])
		total += n
		data = data[n:]
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func TestRejectDuringActiveJsonAndBinaryTransfers(t *testing.T) {
	for _, binary := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "binary"}[binary], func(t *testing.T) {
			a, b := isolatedTransferNode(t, "active-sender"), isolatedTransferNode(t, "active-receiver")
			left, right := net.Pipe()
			attached := make(chan struct{})
			go func() {
				a.attach(pacedTransferConn{left}, Peer{ID: b.id, Name: b.name(), Addr: b.addr}, "test", binary, binary)
				close(attached)
			}()
			b.attach(right, Peer{ID: a.id, Name: a.name(), Addr: a.addr}, "test", binary, binary)
			<-attached
			b.mu.Lock()
			b.cfg.Trust[a.id] = true
			b.mu.Unlock()
			source := filepath.Join(t.TempDir(), "active.bin")
			data := bytes.Repeat([]byte{17, 93, 41}, binaryChunkSize)
			if err := os.WriteFile(source, data, 0600); err != nil {
				t.Fatal(err)
			}
			id, err := a.Send([]string{source}, b.id)
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, func() bool {
				b.mu.Lock()
				defer b.mu.Unlock()
				v := b.cfg.Tasks[id]
				return v != nil && v.Done > 0 && v.Done < v.Total
			})
			if err = b.TaskAction(id, "reject"); err != nil {
				t.Fatal(err)
			}
			waitFor(t, func() bool {
				a.mu.Lock()
				defer a.mu.Unlock()
				return !a.busy[id] && a.cfg.Tasks[id].Status == "rejected"
			})
			b.mu.Lock()
			v := *b.cfg.Tasks[id]
			b.mu.Unlock()
			if v.Status != "rejected" || v.Done != 0 {
				t.Fatal("active transfer resumed after refusal", v.Status, v.Done)
			}
			if _, err = os.Stat(v.Files[0].Temp); !os.IsNotExist(err) {
				t.Fatal("in-flight chunk recreated refused data", err)
			}
		})
	}
}

func TestRejectGuardsAndSafePartialCleanup(t *testing.T) {
	n, err := NewNode(t.TempDir(), "guard", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	protected := filepath.Join(t.TempDir(), "keep.bin")
	os.WriteFile(protected, []byte("keep"), 0600)
	for _, v := range []Task{{Direction: "out", Status: "waiting"}, {Direction: "in", Status: "done"}, {Direction: "in", Status: "verifying"}, {Direction: "in", Kind: "text", Status: "receiving"}} {
		v.ID = rid(16)
		v.Files = []Meta{{Temp: protected, Final: protected}}
		n.cfg.Tasks[v.ID] = &v
		if n.TaskAction(v.ID, "reject") == nil {
			t.Fatal("invalid rejection allowed", v)
		}
	}
	if err = discardRejectedParts(Task{ID: rid(16), Files: []Meta{{Temp: protected}}}); err == nil {
		t.Fatal("unowned cache path accepted")
	}
	if got, err := os.ReadFile(protected); err != nil || string(got) != "keep" {
		t.Fatal("cleanup removed arbitrary file", err)
	}
}
