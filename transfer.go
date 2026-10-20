package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Offer struct {
	Kind       string `json:"kind,omitempty"`
	ID         string `json:"id"`
	Sender     string `json:"sender"`
	SenderName string `json:"senderName"`
	Title      string `json:"title"`
	Folder     bool   `json:"folder"`
	Files      []Meta `json:"files"`
}
type TaskReply struct {
	Status  string  `json:"status"`
	Offsets []int64 `json:"offsets"`
	Error   string  `json:"error"`
}
type Chunk struct {
	ID     string `json:"id"`
	Index  int    `json:"index"`
	Offset int64  `json:"offset"`
	Data   []byte `json:"data"`
}
type ShareRequest struct {
	ID     string `json:"id"`
	Index  int    `json:"index"`
	Offset int64  `json:"offset"`
	Length int    `json:"length"`
}

func cleanFiles(files []Meta) []Meta {
	out := make([]Meta, len(files))
	for i, f := range files {
		out[i] = Meta{Rel: f.Rel, Size: f.Size, SHA: f.SHA}
	}
	return out
}
func marshal(v interface{}) (json.RawMessage, error) { return json.Marshal(v) }
func (n *Node) handle(e *Envelope) (json.RawMessage, error) {
	if e.Target != n.id && e.Method != "offer" {
		return nil, errors.New("网页设备通过页面收发，不提供节点共享接口")
	}
	switch e.Method {
	case "chunk.binary":
		return n.receiveBinaryChunk(e)
	case "phone.names":
		return marshal(n.localPhoneNames())
	case "text.read":
		var v struct{ ID string }
		if err := json.Unmarshal(e.Data, &v); err != nil {
			return nil, err
		}
		n.mu.Lock()
		t := n.cfg.Tasks[v.ID]
		allowed := t != nil && t.Direction == "in" && t.Owner == e.Origin
		n.mu.Unlock()
		if !allowed {
			return nil, errors.New("无权读取此文字任务")
		}
		s, err := n.ReadTaskText(v.ID)
		if err != nil {
			return nil, err
		}
		return marshal(struct {
			Text string `json:"text"`
		}{s})
	case "offer":
		var v Offer
		if err := json.Unmarshal(e.Data, &v); err != nil {
			return nil, err
		}
		return n.offer(e.Origin, e.Target, v)
	case "out.read":
		var v ShareRequest
		json.Unmarshal(e.Data, &v)
		n.mu.Lock()
		t := n.cfg.Tasks[v.ID]
		if t == nil || t.Direction != "out" || !strings.HasPrefix(t.Target, "web:"+e.Origin+":") || v.Index < 0 || v.Index >= len(t.Files) || v.Offset < 0 || v.Length < 1 || v.Length > chunkSize {
			n.mu.Unlock()
			return nil, errors.New("无权读取该发送任务")
		}
		f := t.Files[v.Index]
		n.mu.Unlock()
		st, err := os.Stat(f.Source)
		if err != nil || st.Size() != f.Size || st.ModTime().UnixNano() != f.Mod {
			return nil, errors.New("源文件已改变")
		}
		if v.Offset > f.Size {
			return nil, errors.New("偏移错误")
		}
		count := int64(v.Length)
		if count > f.Size-v.Offset {
			count = f.Size - v.Offset
		}
		data := make([]byte, int(count))
		file, err := os.Open(f.Source)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		_, err = file.ReadAt(data, v.Offset)
		if err != nil && err != io.EOF {
			return nil, err
		}
		return marshal(struct {
			Data []byte `json:"data"`
		}{data})
	case "chunk":
		var v Chunk
		if err := json.Unmarshal(e.Data, &v); err != nil {
			return nil, err
		}
		return n.chunk(e.Origin, v)
	case "finish":
		var v struct{ ID string }
		json.Unmarshal(e.Data, &v)
		return n.finish(e.Origin, v.ID)
	case "shares":
		n.mu.Lock()
		out := []Share{}
		for _, s := range n.cfg.Shares {
			copyS := *s
			copyS.Files = cleanFiles(s.Files)
			out = append(out, copyS)
		}
		n.mu.Unlock()
		return marshal(out)
	case "share.manifest":
		var v ShareRequest
		json.Unmarshal(e.Data, &v)
		n.mu.Lock()
		s := n.cfg.Shares[v.ID]
		if s == nil {
			n.mu.Unlock()
			return nil, errors.New("该分享已移除")
		}
		copyS := *s
		copyS.Files = cleanFiles(s.Files)
		n.mu.Unlock()
		for _, f := range copyS.Files {
			if len(f.SHA) != 64 {
				return nil, errors.New("共享文件正在校验，请稍后重试")
			}
		}
		return marshal(copyS)
	case "share.chunk":
		var v ShareRequest
		json.Unmarshal(e.Data, &v)
		if v.Length < 1 || v.Length > chunkSize || v.Offset < 0 {
			return nil, errors.New("读取范围无效")
		}
		n.mu.Lock()
		s := n.cfg.Shares[v.ID]
		if s == nil || v.Index < 0 || v.Index >= len(s.Files) {
			n.mu.Unlock()
			return nil, errors.New("分享已移除")
		}
		f := s.Files[v.Index]
		n.mu.Unlock()
		st, err := os.Stat(f.Source)
		if err != nil || st.Size() != f.Size || st.ModTime().UnixNano() != f.Mod {
			return nil, errors.New("原文件已移动或改变，请重新分享")
		}
		if v.Offset > f.Size {
			return nil, errors.New("偏移无效")
		}
		size := int64(v.Length)
		if size > f.Size-v.Offset {
			size = f.Size - v.Offset
		}
		data := make([]byte, int(size))
		file, err := os.Open(f.Source)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		_, err = file.ReadAt(data, v.Offset)
		if err != nil && err != io.EOF {
			return nil, err
		}
		return marshal(struct {
			Data []byte `json:"data"`
		}{data})
	default:
		return nil, errors.New("未知节点操作")
	}
}
func (n *Node) replyLocked(t *Task) TaskReply {
	v := TaskReply{Status: t.Status, Error: t.Error}
	for _, f := range t.Files {
		v.Offsets = append(v.Offsets, f.Offset)
	}
	return v
}
func (n *Node) offer(owner, target string, v Offer) (json.RawMessage, error) {
	if v.Kind != "" && v.Kind != "text" {
		return nil, errors.New("不支持的传输类型")
	}
	if v.Kind == "text" && (v.Folder || len(v.Files) != 1 || v.Files[0].Size < 1 || v.Files[0].Size > maxTextBytes) {
		return nil, errors.New("文字消息格式无效或超过 64 KB")
	}
	if decoded, err := hex.DecodeString(v.ID); err != nil || len(decoded) != 16 {
		return nil, errors.New("任务编号无效")
	}
	if len(v.ID) != 32 || len(v.Files) == 0 || len(v.Files) > 5000 {
		return nil, errors.New("任务格式无效")
	}
	if v.Sender != owner && !strings.HasPrefix(v.Sender, "web:"+owner+":") {
		return nil, errors.New("发送者身份无效")
	}
	lock := n.taskLock(v.ID)
	lock.Lock()
	defer lock.Unlock()
	n.mu.Lock()
	existing := n.cfg.Tasks[v.ID]
	if existing != nil {
		if owner == n.id && existing.Direction == "out" && existing.Owner == n.id && existing.Sender == n.id && existing.Target == target && strings.HasPrefix(target, "web:"+n.id+":") {
			reply := TaskReply{Status: "browser"}
			if existing.Status == "done" {
				reply.Status = "done"
			}
			n.mu.Unlock()
			return marshal(reply)
		}
		if (existing.Direction != "in" && existing.Direction != "web") || existing.Owner != owner || existing.Target != target {
			n.mu.Unlock()
			return nil, errors.New("任务身份冲突")
		}
		autoText := existing.Direction == "in" && existing.Kind == "text" && existing.Status == "waiting"
		if autoText {
			n.mu.Unlock()
			if err := n.acceptLocked(existing.ID, false); err != nil {
				return nil, err
			}
			n.mu.Lock()
		}
		reply := n.replyLocked(existing)
		n.mu.Unlock()
		return marshal(reply)
	}
	n.mu.Unlock()
	files := cleanFiles(v.Files)
	var total int64
	seen := map[string]bool{}
	for i := range files {
		f := &files[i]
		rel, err := safeRel(f.Rel)
		if err != nil {
			return nil, err
		}
		f.Rel = filepath.ToSlash(rel)
		if seen[strings.ToLower(f.Rel)] {
			return nil, errors.New("目录中存在重名文件")
		}
		seen[strings.ToLower(f.Rel)] = true
		if f.Size < 0 || f.Size > maxFileSize || len(f.SHA) != 64 {
			return nil, errors.New("文件大小或校验值无效")
		}
		if _, err = hex.DecodeString(f.SHA); err != nil {
			return nil, errors.New("校验值无效")
		}
		total += f.Size
	}
	title, err := safePart(v.Title)
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	direction, status := "in", "waiting"
	if strings.HasPrefix(target, "web:"+n.id+":") {
		direction, status = "web", "browser"
	}
	t := &Task{Kind: v.Kind, ID: v.ID, Direction: direction, Owner: owner, Sender: v.Sender, SenderName: v.SenderName, Target: target, TargetName: n.cfg.Name, Title: title, Folder: v.Folder, Files: files, Status: status, Total: total, Created: time.Now().Unix(), CreatedNS: time.Now().UnixNano()}
	n.cfg.Tasks[t.ID] = t
	trusted := n.cfg.Trust[v.Sender]
	n.mu.Unlock()
	if (trusted || v.Kind == "text") && direction == "in" {
		if err = n.acceptLocked(t.ID, false); err != nil {
			return nil, err
		}
	}
	n.persist()
	n.mu.Lock()
	reply := n.replyLocked(t)
	n.mu.Unlock()
	return marshal(reply)
}
func (n *Node) Accept(id string, trust bool) error {
	l := n.taskLock(id)
	l.Lock()
	defer l.Unlock()
	return n.acceptLocked(id, trust)
}
func (n *Node) acceptLocked(id string, trust bool) error {
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	if t == nil || t.Direction != "in" || t.Status != "waiting" {
		n.mu.Unlock()
		return errors.New("没有待接收任务")
	}
	save := n.cfg.SaveDir
	folder, title, count := t.Folder, t.Title, len(t.Files)
	n.mu.Unlock()
	root := save
	if folder || count > 1 {
		if !folder {
			title = "邻传-" + time.Now().Format("0102-150405")
		}
		for i := 0; i < 10000; i++ {
			name := title
			if i > 0 {
				name = fmt.Sprintf("%s (%d)", title, i)
			}
			root = filepath.Join(save, name)
			e := os.Mkdir(root, 0700)
			if e == nil {
				break
			}
			if !os.IsExist(e) {
				return e
			}
			if i == 9999 {
				return errors.New("同名目录过多")
			}
		}
	}
	tempRoot := filepath.Join(save, ".邻传未完成", id)
	if e := os.MkdirAll(tempRoot, 0700); e != nil {
		return e
	}
	n.mu.Lock()
	t.Root = root
	for i := range t.Files {
		t.Files[i].Temp = filepath.Join(tempRoot, fmt.Sprintf("%d.part", i))
	}
	t.Status = "receiving"
	if trust {
		n.cfg.Trust[t.Sender] = true
	}
	n.mu.Unlock()
	return n.persist()
}
func (n *Node) chunk(owner string, v Chunk) (json.RawMessage, error) {
	return n.chunkWithLimit(owner, v, chunkSize)
}

func (n *Node) chunkWithLimit(owner string, v Chunk, limit int) (json.RawMessage, error) {
	if len(v.Data) > limit || v.Offset < 0 {
		return nil, errors.New("文件块无效")
	}
	lock := n.taskLock(v.ID)
	lock.Lock()
	defer lock.Unlock()
	n.mu.Lock()
	t := n.cfg.Tasks[v.ID]
	if t == nil || t.Direction != "in" || t.Owner != owner || v.Index < 0 || v.Index >= len(t.Files) {
		n.mu.Unlock()
		return nil, errors.New("任务不存在或无权写入")
	}
	if t.Status != "receiving" {
		n.mu.Unlock()
		return nil, errors.New("接收任务尚未允许或已结束")
	}
	f := t.Files[v.Index]
	n.mu.Unlock()
	if v.Offset < f.Offset {
		return marshal(struct {
			Offset int64 `json:"offset"`
		}{f.Offset})
	}
	if v.Offset != f.Offset || int64(len(v.Data)) > f.Size-f.Offset {
		return nil, errors.New("文件块偏移不一致，请重新协商进度")
	}
	if free, e := freeSpace(filepath.Dir(f.Temp)); e == nil && free < uint64(len(v.Data))+1024*1024 {
		return nil, errors.New("接收磁盘空间不足")
	}
	out, e := os.OpenFile(f.Temp, os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	_, e = out.WriteAt(v.Data, v.Offset)
	// Acknowledge OS-cache writes between checkpoints; force durability every
	// 8 MiB and at EOF, rather than stalling the network on every 256 KiB.
	next := v.Offset + int64(len(v.Data))
	if e == nil && (next == f.Size || v.Offset/(8*1024*1024) != next/(8*1024*1024)) {
		e = out.Sync()
	}
	closeE := out.Close()
	if e == nil {
		e = closeE
	}
	if e != nil {
		return nil, e
	}
	n.mu.Lock()
	t.Files[v.Index].Offset += int64(len(v.Data))
	t.Done += int64(len(v.Data))
	offset := t.Files[v.Index].Offset
	n.mu.Unlock()
	return marshal(struct {
		Offset int64 `json:"offset"`
	}{offset})
}
func (n *Node) finish(owner, id string) (json.RawMessage, error) {
	lock := n.taskLock(id)
	lock.Lock()
	defer lock.Unlock()
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	if t == nil || t.Owner != owner || t.Direction != "in" {
		n.mu.Unlock()
		return nil, errors.New("任务不存在或无权完成")
	}
	if t.Status == "done" {
		reply := n.replyLocked(t)
		n.mu.Unlock()
		return marshal(reply)
	}
	if t.Status != "receiving" {
		n.mu.Unlock()
		return nil, errors.New("任务尚未接收")
	}
	files := append([]Meta{}, t.Files...)
	root := t.Root
	kind := t.Kind
	n.mu.Unlock()
	for i, f := range files {
		if f.Final != "" {
			continue
		}
		if f.Offset != f.Size {
			return nil, errors.New("文件尚未传完整")
		}
		if f.Size == 0 {
			file, e := os.OpenFile(f.Temp, os.O_CREATE|os.O_WRONLY, 0600)
			if e != nil {
				return nil, e
			}
			file.Close()
		}
		stable, e := os.OpenFile(f.Temp, os.O_WRONLY, 0600)
		if e != nil {
			return nil, e
		}
		e = stable.Sync()
		closeErr := stable.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			return nil, e
		}
		hash, e := shaFile(f.Temp)
		if e != nil {
			return nil, e
		}
		if hash != f.SHA {
			os.Truncate(f.Temp, 0)
			n.mu.Lock()
			t.Done -= t.Files[i].Offset
			t.Files[i].Offset = 0
			t.Error = "文件校验不一致，已重置该文件进度"
			n.mu.Unlock()
			n.persist()
			return nil, errors.New("文件校验失败，请重试")
		}
		if kind == "text" {
			if _, e := readTextFile(f.Temp, f); e != nil {
				return nil, e
			}
		}
		rel, e := safeRel(f.Rel)
		if e != nil {
			return nil, e
		}
		dest := filepath.Join(root, rel)
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return nil, e
		}
		base, ext := strings.TrimSuffix(dest, filepath.Ext(dest)), filepath.Ext(dest)
		for j := 0; j < 10000; j++ {
			candidate := dest
			if j > 0 {
				candidate = fmt.Sprintf("%s (%d)%s", base, j, ext)
			}
			e = moveNoReplace(f.Temp, candidate)
			if e == nil {
				dest = candidate
				break
			}
			if !os.IsExist(e) {
				return nil, e
			}
		}
		if e != nil {
			return nil, e
		}
		n.mu.Lock()
		t.Files[i].Final = dest
		n.mu.Unlock()
		n.persist()
	}
	n.mu.Lock()
	t.Status = "done"
	t.Error = ""
	t.Rate = 0
	reply := n.replyLocked(t)
	n.mu.Unlock()
	n.persist()
	os.Remove(filepath.Join(filepath.Dir(files[0].Temp)))
	return marshal(reply)
}
func (n *Node) Send(paths []string, target string) (string, error) {
	return n.sendFiles(paths, target, "")
}
func (n *Node) sendFiles(paths []string, target, kind string) (string, error) {
	if target == n.id {
		return "", errors.New("请选择另一台设备")
	}
	files, folder, title, e := collect(paths)
	if e != nil {
		return "", e
	}
	targetName := target
	for _, d := range n.devices() {
		if d.ID == target {
			targetName = d.Name
		}
	}
	t := &Task{Kind: kind, ID: rid(16), Direction: "out", Owner: n.id, Sender: n.id, SenderName: n.name(), Target: target, TargetName: targetName, Title: title, Folder: folder, Files: files, Status: "preparing", Created: time.Now().Unix(), CreatedNS: time.Now().UnixNano()}
	for _, f := range files {
		t.Total += f.Size
	}
	n.mu.Lock()
	n.cfg.Tasks[t.ID] = t
	n.mu.Unlock()
	n.persist()
	n.kick()
	return t.ID, nil
}
func (n *Node) setTask(id, status, msg string) {
	n.mu.Lock()
	if t := n.cfg.Tasks[id]; t != nil {
		t.Status = status
		t.Error = msg
		t.Rate = 0
	}
	n.mu.Unlock()
}
func (n *Node) prepare(id string) error {
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	files := append([]Meta{}, t.Files...)
	n.mu.Unlock()
	for i, f := range files {
		if f.SHA != "" {
			continue
		}
		select {
		case <-n.stop:
			return errors.New("程序正在退出")
		default:
		}
		n.setTask(id, "preparing", "正在计算源文件校验值")
		st, e := os.Stat(f.Source)
		if e != nil || st.Size() != f.Size || st.ModTime().UnixNano() != f.Mod {
			return errors.New("源文件已改变或移动，请重新选择")
		}
		hash, e := shaFile(f.Source)
		if e != nil {
			return e
		}
		st, e = os.Stat(f.Source)
		if e != nil || st.Size() != f.Size || st.ModTime().UnixNano() != f.Mod {
			return errors.New("校验时源文件发生变化，请重新选择")
		}
		n.mu.Lock()
		t.Files[i].SHA = hash
		n.mu.Unlock()
	}
	return n.persist()
}
func (n *Node) sendTask(id string) error {
	if e := n.prepare(id); e != nil {
		return e
	}
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	offer := Offer{Kind: t.Kind, ID: t.ID, Sender: t.Sender, SenderName: t.SenderName, Title: t.Title, Folder: t.Folder, Files: cleanFiles(t.Files)}
	target := t.Target
	files := append([]Meta{}, t.Files...)
	n.mu.Unlock()
	var reply TaskReply
	if e := n.Call(target, "offer", offer, &reply); e != nil {
		return e
	}
	if reply.Status == "browser" {
		n.mu.Lock()
		if t.Status != "done" {
			t.Status = "waiting_browser"
			t.Error = ""
		}
		n.mu.Unlock()
		n.persist()
		return nil
	}
	if reply.Status == "waiting" {
		n.setTask(id, "waiting_remote", "等待对方接收")
		return nil
	}
	if reply.Status == "rejected" {
		n.setTask(id, "rejected", "对方已拒绝")
		return nil
	}
	if reply.Status == "done" {
		n.mu.Lock()
		t.Done = t.Total
		t.Status = "done"
		n.mu.Unlock()
		return nil
	}
	if reply.Status != "receiving" {
		return errors.New("对方任务状态不可继续")
	}
	if len(reply.Offsets) != len(files) {
		return errors.New("接收进度格式错误")
	}
	for i, off := range reply.Offsets {
		if off < 0 || off > files[i].Size {
			return errors.New("接收进度超出文件范围")
		}
	}
	n.mu.Lock()
	t.Status = "sending"
	t.Error = ""
	t.Done = 0
	for i, off := range reply.Offsets {
		t.Files[i].Offset = off
		t.Done += off
	}
	n.mu.Unlock()
	for i, f := range files {
		src, e := os.Open(f.Source)
		if e != nil {
			return e
		}
		st, e := src.Stat()
		if e != nil || st.Size() != f.Size || st.ModTime().UnixNano() != f.Mod {
			src.Close()
			return errors.New("源文件已改变，请重新添加")
		}
		offset := reply.Offsets[i]
		fast := t.Kind != "text" && n.binaryLink(target) != nil
		blockSize := chunkSize
		if fast {
			blockSize = binaryChunkSize
		}
		meter := transferRate{Started: time.Now()}
		for offset < f.Size {
			n.mu.Lock()
			paused := t.Manual
			n.mu.Unlock()
			if paused {
				src.Close()
				return nil
			}
			select {
			case <-n.stop:
				src.Close()
				return errors.New("程序正在退出")
			default:
			}
			count := int64(blockSize)
			if count > f.Size-offset {
				count = f.Size - offset
			}
			data := make([]byte, int(count))
			if _, e = src.ReadAt(data, offset); e != nil {
				src.Close()
				return e
			}
			var ack struct {
				Offset int64 `json:"offset"`
			}
			if fast {
				e = n.sendBinaryChunk(target, Chunk{id, i, offset, data}, &ack)
			} else {
				e = n.Call(target, "chunk", Chunk{id, i, offset, data}, &ack)
			}
			if e != nil {
				src.Close()
				return e
			}
			if ack.Offset <= offset || ack.Offset > f.Size {
				src.Close()
				return errors.New("对方返回的偏移无效")
			}
			n.mu.Lock()
			t.Done += ack.Offset - offset
			t.Files[i].Offset = ack.Offset
			if rate, ready := meter.add(ack.Offset-offset, time.Now()); ready {
				t.Rate = rate
			}
			n.mu.Unlock()
			offset = ack.Offset
		}
		after, e := src.Stat()
		src.Close()
		if e != nil || after.Size() != f.Size || after.ModTime().UnixNano() != f.Mod {
			return errors.New("传输期间源文件发生变化，请重新添加")
		}
	}
	if e := n.Call(target, "finish", struct{ ID string }{id}, &reply); e != nil {
		return e
	}
	n.mu.Lock()
	t.Status = "done"
	t.Done = t.Total
	t.Error = ""
	t.Rate = 0
	n.mu.Unlock()
	return n.persist()
}
func (n *Node) kick() {
	n.mu.Lock()
	select {
	case <-n.stop:
		n.mu.Unlock()
		return
	default:
	}
	for id, t := range n.cfg.Tasks {
		eligible := t.Direction == "out" || t.Pull
		if !eligible || t.Manual || n.busy[id] || t.Status == "done" || t.Status == "rejected" {
			continue
		}
		if len(n.busy) >= 2 {
			break
		}
		n.busy[id] = true
		n.wg.Add(1)
		go func(id string, pull bool) {
			defer n.wg.Done()
			var err error
			if pull {
				err = n.pullTask(id)
			} else {
				err = n.sendTask(id)
			}
			if err != nil {
				n.setTask(id, "paused", err.Error())
			}
			n.mu.Lock()
			delete(n.busy, id)
			n.mu.Unlock()
		}(id, t.Pull)
	}
	n.mu.Unlock()
}
func (n *Node) TaskAction(id, action string) error {
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	if t == nil {
		n.mu.Unlock()
		return errors.New("任务不存在")
	}
	if t.Status == "done" || t.Status == "rejected" {
		n.mu.Unlock()
		return errors.New("任务已经结束，如需再次传输请重新发送")
	}
	switch action {
	case "accept", "trust":
		n.mu.Unlock()
		return n.Accept(id, action == "trust")
	case "reject":
		if t.Status != "waiting" {
			n.mu.Unlock()
			return errors.New("只能拒绝尚未接收的任务")
		}
		t.Status = "rejected"
	case "pause":
		if t.Status == "waiting" {
			n.mu.Unlock()
			return errors.New("请先接收或拒绝这个任务")
		}
		if t.Direction == "webout" || t.Direction == "web" {
			n.mu.Unlock()
			return errors.New("请在对应手机网页上操作")
		}
		t.Manual = true
		t.Status = "paused"
		t.Error = "已手动暂停"
	case "resume":
		if t.Direction == "in" && !t.Pull && t.Root == "" {
			n.mu.Unlock()
			return errors.New("请先确认接收任务")
		}
		t.Manual = false
		if t.Direction == "in" && !t.Pull {
			t.Status = "receiving"
		} else {
			t.Status = "queued"
		}
		t.Error = ""
	default:
		n.mu.Unlock()
		return errors.New("未知操作")
	}
	n.mu.Unlock()
	n.persist()
	n.kick()
	return nil
}
func (n *Node) AddShare(paths []string) error {
	files, folder, title, e := collect(paths)
	if e != nil {
		return e
	}
	s := &Share{ID: rid(16), Name: title, Folder: folder, Files: files}
	for _, f := range files {
		s.Size += f.Size
	}
	n.mu.Lock()
	n.cfg.Shares[s.ID] = s
	n.mu.Unlock()
	n.persist()
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		for i, f := range files {
			hash, e := shaFile(f.Source)
			if e != nil {
				return
			}
			n.mu.Lock()
			s.Files[i].SHA = hash
			n.mu.Unlock()
		}
		n.persist()
	}()
	return nil
}
func (n *Node) Pull(target, shareID string) (string, error) {
	var share Share
	if e := n.Call(target, "share.manifest", ShareRequest{ID: shareID}, &share); e != nil {
		return "", e
	}
	id := rid(16)
	name := target
	for _, d := range n.devices() {
		if d.ID == target {
			name = d.Name
		}
	}
	t := &Task{ID: id, Direction: "in", Owner: n.id, Sender: n.id, SenderName: name, Target: target, TargetName: n.name(), Title: share.Name, Folder: share.Folder, Files: share.Files, Status: "waiting", Created: time.Now().Unix(), CreatedNS: time.Now().UnixNano(), Pull: true, ShareID: shareID}
	for _, f := range t.Files {
		t.Total += f.Size
	}
	n.mu.Lock()
	n.cfg.Tasks[id] = t
	n.mu.Unlock()
	if e := n.Accept(id, false); e != nil {
		return "", e
	}
	n.kick()
	return id, nil
}
func (n *Node) pullTask(id string) error {
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	files := append([]Meta{}, t.Files...)
	target, shareID := t.Target, t.ShareID
	if t.Status == "paused" || t.Status == "queued" {
		t.Status = "receiving"
	}
	n.mu.Unlock()
	for i, f := range files {
		offset := f.Offset
		for offset < f.Size {
			n.mu.Lock()
			manual := t.Manual
			n.mu.Unlock()
			if manual {
				return nil
			}
			var reply struct {
				Data []byte `json:"data"`
			}
			start := time.Now()
			if e := n.Call(target, "share.chunk", ShareRequest{ID: shareID, Index: i, Offset: offset, Length: chunkSize}, &reply); e != nil {
				return e
			}
			if len(reply.Data) == 0 {
				return errors.New("共享文件提前结束")
			}
			raw, e := n.chunk(n.id, Chunk{id, i, offset, reply.Data})
			if e != nil {
				return e
			}
			var ack struct {
				Offset int64 `json:"offset"`
			}
			json.Unmarshal(raw, &ack)
			n.mu.Lock()
			t.Rate = float64(ack.Offset-offset) / time.Since(start).Seconds()
			n.mu.Unlock()
			offset = ack.Offset
		}
	}
	_, e := n.finish(n.id, id)
	return e
}
func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
