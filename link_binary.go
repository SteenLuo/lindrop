package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"time"
)

const binaryPacketFlag uint32 = 1 << 31
const binaryChunkSize = 1024 * 1024
const binaryHeaderLimit = 16 * 1024

// The signature covers the bounded metadata, including the exact payload hash.
// Only negotiated, directly authenticated TLS peers use this format. Relays and
// older nodes retain the original signed JSON messages unchanged.
type binaryChunkHeader struct {
	ID     string `json:"id"`
	Index  int    `json:"index"`
	Offset int64  `json:"offset"`
	Length int    `json:"length"`
	SHA    string `json:"sha"`
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if n < 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func writeBinaryPacket(w io.Writer, f Frame) error {
	if f.Kind != "request" || f.Request == nil || f.Request.Method != "chunk.binary" || len(f.Request.Binary) < 1 || len(f.Request.Binary) > binaryChunkSize {
		return errors.New("二进制帧格式无效")
	}
	header, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if len(header) > binaryHeaderLimit {
		return errors.New("二进制元数据过大")
	}
	var prefix [8]byte
	binary.BigEndian.PutUint32(prefix[:4], binaryPacketFlag|uint32(4+len(header)+len(f.Request.Binary)))
	binary.BigEndian.PutUint32(prefix[4:], uint32(len(header)))
	if err = writeAll(w, prefix[:]); err != nil {
		return err
	}
	if err = writeAll(w, header); err != nil {
		return err
	}
	return writeAll(w, f.Request.Binary)
}

func readBinaryPacket(r io.Reader, size uint32, out interface{}) error {
	f, ok := out.(*Frame)
	if !ok || size < 6 || size > 4+binaryHeaderLimit+binaryChunkSize {
		return errors.New("二进制帧长度无效")
	}
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if n == 0 || n > binaryHeaderLimit || n+4 >= size || size-n-4 > binaryChunkSize {
		return errors.New("二进制元数据长度无效")
	}
	header := make([]byte, int(n))
	if _, err := io.ReadFull(r, header); err != nil {
		return err
	}
	if err := json.Unmarshal(header, f); err != nil {
		return err
	}
	if f.Kind != "request" || f.Request == nil || f.Request.Method != "chunk.binary" {
		return errors.New("二进制帧操作无效")
	}
	f.Request.Binary = make([]byte, int(size-n-4))
	_, err := io.ReadFull(r, f.Request.Binary)
	return err
}

func (n *Node) binaryLink(target string) *Link {
	n.mu.Lock()
	defer n.mu.Unlock()
	l := n.links[target]
	if l != nil && l.binaryChunks {
		return l
	}
	return nil
}

func (n *Node) sendBinaryChunk(target string, v Chunk, out interface{}) error {
	l := n.binaryLink(target)
	if l == nil {
		return errors.New("直接连接已改变，请重新协商进度")
	}
	if len(v.Data) < 1 || len(v.Data) > binaryChunkSize {
		return errors.New("二进制块大小无效")
	}
	raw, err := json.Marshal(binaryChunkHeader{v.ID, v.Index, v.Offset, len(v.Data), hashBytes(v.Data)})
	if err != nil {
		return err
	}
	e := &Envelope{ID: rid(16), Origin: n.id, Pub: n.pub, Time: time.Now().Unix(), Target: target, Method: "chunk.binary", Data: raw, Binary: v.Data, Visited: []string{n.id}}
	e.Signature, err = ecdsa.SignASN1(rand.Reader, n.key, signatureBody(e))
	if err != nil {
		return err
	}
	raw, err = l.call(e)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (n *Node) receiveBinaryChunk(e *Envelope) (json.RawMessage, error) {
	var v binaryChunkHeader
	if err := json.Unmarshal(e.Data, &v); err != nil {
		return nil, err
	}
	if e.Target != n.id || len(e.Visited) != 1 || e.Visited[0] != e.Origin || v.Length < 1 || v.Length > binaryChunkSize || v.Length != len(e.Binary) || hashBytes(e.Binary) != v.SHA {
		return nil, errors.New("二进制块内容或直接路径校验失败")
	}
	return n.chunkWithLimit(e.Origin, Chunk{v.ID, v.Index, v.Offset, e.Binary}, binaryChunkSize)
}
