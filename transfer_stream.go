package main

import (
	"encoding/hex"
	"errors"
	"hash"
	"os"
)

type FinishRequest struct {
	ID     string   `json:"id"`
	Hashes []string `json:"hashes,omitempty"`
}

func validFileSHA(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (n *Node) canStreamHashes(target string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	l := n.links[target]
	return l != nil && l.binaryChunks && l.streamHashes
}

func (n *Node) prepareTransfer(id string) (bool, error) {
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	if t == nil {
		n.mu.Unlock()
		return false, errors.New("发送任务不存在")
	}
	target, kind := t.Target, t.Kind
	files := append([]Meta(nil), t.Files...)
	n.mu.Unlock()
	stream := kind == "" && n.canStreamHashes(target)
	n.mu.Lock()
	t.HashAtFinish = stream
	n.mu.Unlock()
	if !stream {
		return false, n.prepare(id)
	}
	for _, f := range files {
		st, err := os.Stat(f.Source)
		if err != nil || st.Size() != f.Size || st.ModTime().UnixNano() != f.Mod {
			return false, errors.New("源文件已改变或移动，请重新选择")
		}
	}
	n.setTask(id, "queued", "")
	return true, n.persist()
}

// Fresh transfers hash only the bytes being sent. On resume, include the
// already acknowledged prefix as well so finish still authenticates the whole
// source file. The prefix scan is cancellable and is never needed at offset 0.
func (n *Node) hashSourcePrefix(id string, src *os.File, size int64, digest hash.Hash) error {
	if size == 0 {
		return nil
	}
	n.setTask(id, "preparing", "正在校验续传前缀")
	buf := make([]byte, chunkSize)
	for off := int64(0); off < size; {
		select {
		case <-n.stop:
			return errors.New("程序正在退出")
		default:
		}
		n.mu.Lock()
		paused := n.cfg.Tasks[id].Manual
		n.mu.Unlock()
		if paused {
			return errors.New("已手动暂停")
		}
		count := int64(len(buf))
		if count > size-off {
			count = size - off
		}
		data := buf[:int(count)]
		if _, err := src.ReadAt(data, off); err != nil {
			return err
		}
		digest.Write(data)
		off += count
	}
	n.setTask(id, "sending", "")
	return nil
}
