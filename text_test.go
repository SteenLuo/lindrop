package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTextRelayAndBounds(t *testing.T) {
	a, b, c := testNode(t, "文字甲"), testNode(t, "中转"), testNode(t, "文字乙")
	connect(t, a, b)
	connect(t, c, b)
	waitFor(t, func() bool { a.mu.Lock(); defer a.mu.Unlock(); _, ok := a.routesLocked()[c.id]; return ok })
	body := "中文与 emoji 😀\n<script>只按文字显示</script>\r\n" + strings.Repeat("长文字保留换行\n", 800)
	id, e := a.SendText(body, c.id)
	if e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.cfg.Tasks[id] != nil })
	waitFor(t, func() bool { return taskDone(a, id) && taskDone(c, id) })
	got, e := c.ReadTaskText(id)
	if e != nil || got != body {
		t.Fatalf("text changed: %v, length %d/%d", e, len(got), len(body))
	}
	b.mu.Lock()
	relayTasks := len(b.cfg.Tasks)
	b.mu.Unlock()
	if relayTasks != 0 {
		t.Fatal("relay saved a message task")
	}
	back, e := c.SendText("反向回复\n第二行", a.id)
	if e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.cfg.Tasks[back] != nil })
	waitFor(t, func() bool { return taskDone(a, back) && taskDone(c, back) })
	if got, e = a.ReadTaskText(back); e != nil || got != "反向回复\n第二行" {
		t.Fatal("reverse text mismatch", e)
	}
	for _, bad := range []string{"", " \n\t", "a" + string(rune(0)), string([]byte{0xff}), strings.Repeat("x", maxTextBytes+1)} {
		if validateText(bad) == nil {
			t.Fatal("invalid text accepted")
		}
	}
	if e = validateText(strings.Repeat("x", maxTextBytes)); e != nil {
		t.Fatal(e)
	}
	for _, v := range []Offer{
		{Kind: "text", Files: []Meta{{Size: maxTextBytes + 1}}},
		{Kind: "text", Files: []Meta{{Size: 1}, {Size: 1}}},
		{Kind: "text", Folder: true, Files: []Meta{{Size: 1}}},
	} {
		if _, e = c.offer(a.id, c.id, v); e == nil {
			t.Fatal("bad typed offer accepted")
		}
	}
}

func webJSON(t *testing.T, client *http.Client, endpoint string, body interface{}) []byte {
	t.Helper()
	raw, _ := json.Marshal(body)
	r, e := client.Post(endpoint, "application/json", bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	data, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 {
		t.Fatalf("%s: %s %s", endpoint, r.Status, data)
	}
	return data
}

func webTextRead(t *testing.T, client *http.Client, base, id string, wantStatus int) string {
	t.Helper()
	r, e := client.Get(base + "/api/text?id=" + id)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	if r.StatusCode != wantStatus {
		data, _ := io.ReadAll(r.Body)
		t.Fatalf("text status %d, want %d: %s", r.StatusCode, wantStatus, data)
	}
	var v struct{ Text string }
	json.NewDecoder(r.Body).Decode(&v)
	return v.Text
}

func TestWebTextTransferAndOwnership(t *testing.T) {
	entry, target := testNode(t, "入口"), testNode(t, "接收电脑")
	connect(t, entry, target)
	first, second := browser(t, entry, "文字手机甲"), browser(t, entry, "文字手机乙")
	base := "http://" + entry.addr
	body := "手机直发文字\n完整保留 😀 <b>原样文字</b>"
	for _, dst := range []*Node{entry, target} {
		id := rid(16)
		offer := Offer{Kind: "text", ID: id, Title: "手机文字.txt", Files: []Meta{{Rel: "手机文字.txt", Size: int64(len(body)), SHA: hashBytes([]byte(body))}}}
		offered := webJSON(t, first, base+"/api/task/offer", map[string]interface{}{"target": dst.id, "offer": offer})
		var response struct{ Reply TaskReply }
		if e := json.Unmarshal(offered, &response); e != nil || response.Reply.Status != "receiving" {
			t.Fatalf("text did not auto-accept: %s", offered)
		}
		webTextRead(t, second, base, id, 403)
		webTextRead(t, first, base, id, 403)
		req, _ := http.NewRequest("POST", base+"/api/task/chunk?id="+id+"&index=0&offset=0", strings.NewReader(body))
		req.Header.Set("X-LinNode-Upload", "1")
		r, e := first.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatal(r.Status)
		}
		webJSON(t, first, base+"/api/task/finish", map[string]string{"id": id})
		if got := webTextRead(t, first, base, id, 200); got != body {
			t.Fatal("web sent text changed")
		}
		webTextRead(t, second, base, id, 403)
	}
	entry.mu.Lock()
	var phoneID string
	for _, c := range entry.cfg.Clients {
		if c.Name == "文字手机甲" {
			phoneID = "web:" + entry.id + ":" + c.ID
		}
	}
	entry.mu.Unlock()
	waitFor(t, func() bool {
		target.mu.Lock()
		defer target.mu.Unlock()
		_, ok := target.routesLocked()[phoneID]
		return ok
	})
	id, e := target.SendText("电脑发给手机\n😀", phoneID)
	if e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool { entry.mu.Lock(); defer entry.mu.Unlock(); return entry.cfg.Tasks[id] != nil })
	webTextRead(t, second, base, id, 403)
	if got := webTextRead(t, first, base, id, 200); got != "电脑发给手机\n😀" {
		t.Fatal("web received text changed")
	}
	waitFor(t, func() bool { return taskDone(target, id) })
}

func TestEntryComputerSendsToOwnPhone(t *testing.T) {
	n := testNode(t, "入口电脑")
	first, second := browser(t, n, "本机手机"), browser(t, n, "其他手机")
	n.mu.Lock()
	phone := ""
	for _, c := range n.cfg.Clients {
		if c.Name == "本机手机" {
			phone = "web:" + n.id + ":" + c.ID
		}
	}
	n.mu.Unlock()
	id, e := n.SendText("入口电脑直接回复手机\n你好 😀", phone)
	if e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool {
		n.mu.Lock()
		defer n.mu.Unlock()
		s := n.cfg.Tasks[id].Status
		return s == "waiting_browser" || s == "paused"
	})
	n.mu.Lock()
	task := *n.cfg.Tasks[id]
	n.mu.Unlock()
	if task.Status == "paused" {
		t.Fatalf("local phone transfer failed: %s", task.Error)
	}
	base := "http://" + n.addr
	webTextRead(t, second, base, id, 403)
	if got := webTextRead(t, first, base, id, 200); got != "入口电脑直接回复手机\n你好 😀" {
		t.Fatal("local phone text changed")
	}
	if !taskDone(n, id) {
		t.Fatal("phone read did not complete sender")
	}
	path := filepath.Join(t.TempDir(), "手机文件.txt")
	want := []byte("本机电脑直接发给手机的文件\n")
	if e = os.WriteFile(path, want, 0600); e != nil {
		t.Fatal(e)
	}
	id, e = n.Send([]string{path}, phone)
	if e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool { n.mu.Lock(); defer n.mu.Unlock(); return n.cfg.Tasks[id].Status == "waiting_browser" })
	r, e := second.Get(base + "/api/download?inbox=" + id)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("other phone accessed local file")
	}
	r, e = first.Get(base + "/api/download?inbox=" + id)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(r.Body)
	r.Body.Close()
	if e != nil || r.StatusCode != 200 || !bytes.Equal(raw, want) {
		t.Fatal("local phone download changed")
	}
	if !taskDone(n, id) {
		t.Fatal("download did not complete sender")
	}
}

func TestTextAutoReceiveKeepsFileConfirmation(t *testing.T) {
	a, b := testNode(t, "发送电脑"), testNode(t, "接收电脑")
	connect(t, a, b)
	id, err := a.SendText("无需点击接收\n中文 😀", b.id)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return taskDone(a, id) && taskDone(b, id) })
	if body, err := b.ReadTaskText(id); err != nil || body != "无需点击接收\n中文 😀" {
		t.Fatal("automatic text changed", err)
	}
	b.mu.Lock()
	trusted := b.cfg.Trust[a.id]
	b.mu.Unlock()
	if trusted {
		t.Fatal("receiving text trusted later file transfers")
	}
	path := filepath.Join(t.TempDir(), "普通文本文件.txt")
	if err = os.WriteFile(path, []byte("这是文件，不是文字消息"), 0600); err != nil {
		t.Fatal(err)
	}
	fileID, err := a.Send([]string{path}, b.id)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		task := b.cfg.Tasks[fileID]
		return task != nil && task.Status == "waiting"
	})
	b.mu.Lock()
	task := *b.cfg.Tasks[fileID]
	b.mu.Unlock()
	if task.Kind != "" || task.Root != "" || task.Done != 0 {
		t.Fatal("file auto-accepted after text")
	}
}

func TestPendingTextUpgradeAndReoffer(t *testing.T) {
	dir, save := t.TempDir(), t.TempDir()
	n, err := NewNode(dir, "升级接收端", save)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("升级前等待接收的文字")
	makeTask := func(kind, status string) *Task {
		return &Task{ID: rid(16), Kind: kind, Direction: "in", Owner: "sender", Sender: "sender", Target: n.id, Title: "消息.txt", Files: []Meta{{Rel: "消息.txt", Size: int64(len(body)), SHA: hashBytes(body)}}, Total: int64(len(body)), Status: status}
	}
	text, file, paused := makeTask("text", "waiting"), makeTask("", "waiting"), makeTask("text", "paused")
	for _, task := range []*Task{text, file, paused} {
		n.cfg.Tasks[task.ID] = task
	}
	if err = n.persist(); err != nil {
		t.Fatal(err)
	}
	n.Close()
	reopened, err := NewNode(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.Start("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if reopened.cfg.Tasks[text.ID].Status != "receiving" || reopened.cfg.Tasks[file.ID].Status != "waiting" || reopened.cfg.Tasks[paused.ID].Status != "paused" {
		t.Fatal("upgrade changed wrong task states")
	}
	if len(reopened.cfg.Trust) != 0 {
		t.Fatal("upgrade changed device trust")
	}
	// A sender retry also upgrades a task left by an earlier receiver version.
	retry := makeTask("text", "waiting")
	reopened.mu.Lock()
	reopened.cfg.Tasks[retry.ID] = retry
	reopened.mu.Unlock()
	raw, err := reopened.offer("sender", reopened.id, Offer{ID: retry.ID, Kind: "text", Sender: "sender", Title: retry.Title, Files: retry.Files})
	if err != nil {
		t.Fatal(err)
	}
	var reply TaskReply
	if err = json.Unmarshal(raw, &reply); err != nil || reply.Status != "receiving" {
		t.Fatal("old pending offer was not accepted", err)
	}
}
