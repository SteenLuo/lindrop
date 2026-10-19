package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const maxTextBytes = 64 * 1024

// Upgrade previously offered text without trusting the sender for file transfers.
// Paused or rejected messages remain under the user's control.
func (n *Node) autoAcceptPendingTexts() {
	n.mu.Lock()
	var ids []string
	for id, t := range n.cfg.Tasks {
		if t.Direction == "in" && t.Kind == "text" && t.Status == "waiting" && !t.Folder && len(t.Files) == 1 && t.Files[0].Size > 0 && t.Files[0].Size <= maxTextBytes {
			ids = append(ids, id)
		}
	}
	n.mu.Unlock()
	for _, id := range ids {
		if err := n.Accept(id, false); err != nil {
			n.mu.Lock()
			if t := n.cfg.Tasks[id]; t != nil && t.Status == "waiting" {
				t.Error = "文字自动接收失败：" + err.Error()
			}
			n.mu.Unlock()
			n.persist()
		}
	}
}

func validateText(s string) error {
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return errors.New("文字包含无效字符")
	}
	if strings.TrimSpace(s) == "" {
		return errors.New("请先输入要发送的文字")
	}
	if len(s) > maxTextBytes {
		return errors.New("文字最多 64 KB，请缩短内容或改为发送文件")
	}
	return nil
}

func (n *Node) SendText(s, target string) (string, error) {
	if e := validateText(s); e != nil {
		return "", e
	}
	if target == "" || target == n.id {
		return "", errors.New("请选择另一台在线设备")
	}
	dir := filepath.Join(n.dir, "text-outbox", rid(16))
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	path := filepath.Join(dir, "文字消息-"+time.Now().Format("20060102-150405")+".txt")
	if e := os.WriteFile(path, []byte(s), 0600); e != nil {
		os.Remove(dir)
		return "", e
	}
	id, e := n.sendFiles([]string{path}, target, "text")
	if e != nil {
		os.Remove(path)
		os.Remove(dir)
	}
	return id, e
}

func readTextFile(path string, f Meta) (string, error) {
	if f.Size < 1 || f.Size > maxTextBytes || path == "" {
		return "", errors.New("文字消息尚不可查看")
	}
	file, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer file.Close()
	raw, e := io.ReadAll(io.LimitReader(file, maxTextBytes+1))
	if e != nil {
		return "", e
	}
	return checkedText(raw, f)
}

func checkedText(raw []byte, f Meta) (string, error) {
	if int64(len(raw)) != f.Size || hashBytes(raw) != f.SHA {
		return "", errors.New("文字内容已改变或校验失败")
	}
	s := string(raw)
	return s, validateText(s)
}

func (n *Node) ReadTaskText(id string) (string, error) {
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	if t == nil || t.Kind != "text" || len(t.Files) != 1 {
		n.mu.Unlock()
		return "", errors.New("不是可查看的文字消息")
	}
	f, direction, status := t.Files[0], t.Direction, t.Status
	n.mu.Unlock()
	if direction == "out" {
		return readTextFile(f.Source, f)
	}
	if direction != "in" || status != "done" {
		return "", errors.New("文字尚未接收完成，请稍后查看")
	}
	return readTextFile(f.Final, f)
}

func (n *Node) readWebText(id string, c WebClient, admin bool) (string, error) {
	if admin {
		return n.ReadTaskText(id)
	}
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	sid := "web:" + n.id + ":" + c.ID
	if t == nil || t.Kind != "text" || len(t.Files) != 1 || (t.Target != sid && t.Client != c.ID) {
		n.mu.Unlock()
		return "", errors.New("无权查看此文字消息")
	}
	copyT := *t
	f := t.Files[0]
	n.mu.Unlock()
	if copyT.Direction == "out" && copyT.Owner == n.id && copyT.Sender == n.id && copyT.Target == sid {
		s, e := readTextFile(f.Source, f)
		if e == nil {
			n.mu.Lock()
			if current := n.cfg.Tasks[id]; current != nil {
				current.Status = "done"
				current.Done = current.Total
				current.Error = ""
			}
			n.mu.Unlock()
			n.persist()
		}
		return s, e
	}
	if copyT.Direction == "in" {
		return n.ReadTaskText(id)
	}
	if copyT.Direction == "webout" {
		var reply struct {
			Text string `json:"text"`
		}
		if e := n.Call(copyT.Target, "text.read", struct{ ID string }{id}, &reply); e != nil {
			return "", e
		}
		return checkedText([]byte(reply.Text), f)
	}
	if copyT.Direction != "web" || f.Size < 1 || f.Size > maxTextBytes {
		return "", errors.New("此任务不支持文字查看")
	}
	var reply struct {
		Data []byte `json:"data"`
	}
	if e := n.Call(copyT.Owner, "out.read", ShareRequest{ID: id, Index: 0, Length: int(f.Size)}, &reply); e != nil {
		return "", e
	}
	s, e := checkedText(reply.Data, f)
	if e == nil {
		n.mu.Lock()
		if current := n.cfg.Tasks[id]; current != nil {
			current.Status, current.Done = "done", current.Total
		}
		n.mu.Unlock()
		n.persist()
	}
	return s, e
}
