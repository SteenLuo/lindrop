package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Envelope struct {
	Binary    []byte          `json:"-"`
	ID        string          `json:"id"`
	Origin    string          `json:"origin"`
	Pub       []byte          `json:"pub"`
	Signature []byte          `json:"signature"`
	Time      int64           `json:"time"`
	Target    string          `json:"target"`
	Method    string          `json:"method"`
	Data      json.RawMessage `json:"data"`
	Visited   []string        `json:"visited"`
}
type Frame struct {
	Kind    string          `json:"kind"`
	ID      string          `json:"id,omitempty"`
	Request *Envelope       `json:"request,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
	Routes  []Route         `json:"routes,omitempty"`
}
type Hello struct {
	BinaryChunks bool
	ID           string
	Name         string
	Addr         string
	Code         string
	Nonce        string
	Error        string
}
type Link struct {
	binaryChunks bool
	n            *Node
	conn         net.Conn
	peer         Peer
	key          string
	routes       []Route
	writeMu      sync.Mutex
	mu           sync.Mutex
	pending      map[string]chan Frame
	done         chan struct{}
	once         sync.Once
	slots        chan struct{}
}

func writePacket(w io.Writer, v interface{}) error {
	if f, ok := v.(Frame); ok && f.Request != nil && f.Request.Binary != nil {
		return writeBinaryPacket(w, f)
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if len(raw) > 2*1024*1024 {
		return errors.New("消息过大")
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(raw)))
	if _, e = w.Write(size[:]); e != nil {
		return e
	}
	_, e = w.Write(raw)
	return e
}
func readPacket(r io.Reader, v interface{}) error {
	var size [4]byte
	if _, e := io.ReadFull(r, size[:]); e != nil {
		return e
	}
	n := binary.BigEndian.Uint32(size[:])
	if n&binaryPacketFlag != 0 {
		return readBinaryPacket(r, n&^binaryPacketFlag, v)
	}
	if n == 0 || n > 2*1024*1024 {
		return errors.New("消息长度无效")
	}
	raw := make([]byte, n)
	if _, e := io.ReadFull(r, raw); e != nil {
		return e
	}
	return json.Unmarshal(raw, v)
}
func (l *Link) send(f Frame) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	l.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	return writePacket(l.conn, f)
}
func (l *Link) close() {
	l.once.Do(func() {
		close(l.done)
		l.conn.Close()
		l.n.mu.Lock()
		if l.n.links[l.peer.ID] == l {
			delete(l.n.links, l.peer.ID)
		}
		l.n.mu.Unlock()
	})
}
func (l *Link) call(e *Envelope) (json.RawMessage, error) {
	id := rid(12)
	ch := make(chan Frame, 1)
	l.mu.Lock()
	l.pending[id] = ch
	l.mu.Unlock()
	defer func() { l.mu.Lock(); delete(l.pending, id); l.mu.Unlock() }()
	if err := l.send(Frame{Kind: "request", ID: id, Request: e}); err != nil {
		l.close()
		return nil, err
	}
	timeout := 45 * time.Second
	if e.Method == "finish" {
		timeout = 30 * time.Minute
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case f := <-ch:
		if f.Error != "" {
			return nil, errors.New(f.Error)
		}
		return f.Data, nil
	case <-l.done:
		return nil, errors.New("连接已断开")
	case <-timer.C:
		return nil, errors.New("对方响应超时")
	}
}
func (l *Link) run() {
	defer l.close()
	for {
		l.conn.SetReadDeadline(time.Now().Add(20 * time.Second))
		var f Frame
		if e := readPacket(l.conn, &f); e != nil {
			return
		}
		switch f.Kind {
		case "routes":
			l.n.mu.Lock()
			l.routes = f.Routes
			for _, r := range f.Routes {
				if r.ID == l.peer.ID {
					p := l.n.cfg.Peers[r.ID]
					p.Name = r.Name
					l.peer.Name = r.Name
					l.n.cfg.Peers[r.ID] = p
					break
				}
			}
			l.n.mu.Unlock()
		case "response":
			l.mu.Lock()
			ch := l.pending[f.ID]
			l.mu.Unlock()
			if ch != nil {
				select {
				case ch <- f:
				default:
				}
			}
		case "request":
			if f.Request == nil {
				continue
			}
			if f.Request.Binary != nil && (!l.binaryChunks || f.Request.Origin != l.peer.ID || f.Request.Target != l.n.id) {
				l.send(Frame{Kind: "response", ID: f.ID, Error: "二进制块必须来自协商过的直接设备"})
				continue
			}
			select {
			case l.slots <- struct{}{}:
			default:
				l.send(Frame{Kind: "response", ID: f.ID, Error: "对方任务繁忙，请稍后重试"})
				continue
			}
			go func(f Frame) {
				defer func() { <-l.slots }()
				data, e := l.n.forward(f.Request)
				reply := Frame{Kind: "response", ID: f.ID, Data: data}
				if e != nil {
					reply.Error = e.Error()
				}
				l.send(reply)
			}(f)
		}
	}
}
func (n *Node) attach(conn net.Conn, peer Peer, key string, binaryChunks bool) {
	l := &Link{n: n, conn: conn, peer: peer, key: key, binaryChunks: binaryChunks, pending: map[string]chan Frame{}, done: make(chan struct{}), slots: make(chan struct{}, 16)}
	n.mu.Lock()
	select {
	case <-n.stop:
		n.mu.Unlock()
		conn.Close()
		return
	default:
	}
	old := n.links[peer.ID]
	if old != nil && old.key < key {
		n.mu.Unlock()
		conn.Close()
		return
	}
	n.links[peer.ID] = l
	for id, p := range n.cfg.Peers {
		if id != peer.ID && peer.Addr != "" && p.Addr == peer.Addr {
			p.Addr = ""
			n.cfg.Peers[id] = p
		}
	}
	n.cfg.Peers[peer.ID] = peer
	n.mu.Unlock()
	if old != nil {
		old.close()
	}
	n.persist()
	go l.run()
	n.announce()
}
func (n *Node) announce() {
	n.mu.Lock()
	routes := n.routesLocked()
	var list []Route
	for _, r := range routes {
		list = append(list, r)
	}
	var links []*Link
	for _, l := range n.links {
		links = append(links, l)
	}
	n.mu.Unlock()
	for _, l := range links {
		if e := l.send(Frame{Kind: "routes", Routes: list}); e != nil {
			l.close()
		}
	}
}
func certID(c *x509.Certificate) string {
	pub, _ := x509.MarshalPKIXPublicKey(c.PublicKey)
	return identity(pub)
}
func (n *Node) Dial(addr, code string) error { return n.dialExpected(addr, code, "") }

func (n *Node) dialExpected(addr, code, expectedID string) error {
	addr = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://"))
	addr = strings.TrimRight(addr, "/")
	if !strings.Contains(addr, ":") {
		addr += ":8776"
	}
	if !privateAddr(addr) {
		return errors.New("请输入局域网 IPv4 地址和端口")
	}
	c, e := net.DialTimeout("tcp4", addr, 5*time.Second)
	if e != nil {
		return fmt.Errorf("无法连接 %s: %w", addr, e)
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	c.SetDeadline(time.Now().Add(12 * time.Second))
	fmt.Fprintf(c, "GET /node/link HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: linnode\r\n\r\n", addr)
	br := bufio.NewReader(c)
	resp, e := http.ReadResponse(br, nil)
	if e != nil {
		return e
	}
	if resp.StatusCode != 101 {
		return errors.New("对方不是可连接的邻传节点")
	}
	if br.Buffered() != 0 {
		return errors.New("握手数据异常")
	}
	tc := tls.Client(c, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{n.cert}, InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) != 1 {
			return errors.New("设备证书无效")
		}
		id := certID(cs.PeerCertificates[0])
		if expectedID != "" && id != expectedID {
			return errors.New("该地址的设备身份已改变，等待重新发现")
		}
		return nil
	}})
	if e = tc.Handshake(); e != nil {
		return e
	}
	targetID := certID(tc.ConnectionState().PeerCertificates[0])
	hello := Hello{ID: n.id, Name: n.name(), Addr: n.addr, Code: code, Nonce: rid(16), BinaryChunks: true}
	if e = writePacket(tc, hello); e != nil {
		return e
	}
	var reply Hello
	if e = readPacket(tc, &reply); e != nil {
		return e
	}
	if reply.Error != "" {
		return errors.New(reply.Error)
	}
	if reply.ID != targetID || reply.ID == n.id {
		return errors.New("设备身份不正确或连接到本机")
	}
	tc.SetDeadline(time.Time{})
	ok = true
	n.attach(tc, Peer{targetID, reply.Name, addr}, hello.Nonce, reply.BinaryChunks)
	return nil
}
func (n *Node) acceptLink(w http.ResponseWriter, r *http.Request) {
	h, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "upgrade unsupported", 500)
		return
	}
	conn, rw, e := h.Hijack()
	if e != nil {
		return
	}
	fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: linnode\r\n\r\n")
	rw.Flush()
	go func() {
		ok := false
		defer func() {
			if !ok {
				conn.Close()
			}
		}()
		conn.SetDeadline(time.Now().Add(12 * time.Second))
		tc := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{n.cert}, ClientAuth: tls.RequireAnyClientCert})
		if tc.Handshake() != nil {
			return
		}
		cs := tc.ConnectionState()
		if len(cs.PeerCertificates) != 1 {
			return
		}
		peerID := certID(cs.PeerCertificates[0])
		var hello Hello
		if readPacket(tc, &hello) != nil {
			return
		}
		if hello.ID != peerID || peerID == n.id {
			return
		}
		remote, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		ip := net.ParseIP(remote)
		if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
			return
		}
		peerAddr := hello.Addr
		if !privateAddr(peerAddr) {
			peerAddr = ""
		}
		if writePacket(tc, Hello{ID: n.id, Name: n.name(), Addr: n.addr, BinaryChunks: true}) != nil {
			return
		}
		tc.SetDeadline(time.Time{})
		ok = true
		n.attach(tc, Peer{peerID, hello.Name, peerAddr}, hello.Nonce, hello.BinaryChunks)
	}()
}
func signatureBody(e *Envelope) []byte {
	v := struct {
		ID, Origin, Target, Method string
		Time                       int64
		Data                       json.RawMessage
	}{e.ID, e.Origin, e.Target, e.Method, e.Time, e.Data}
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return h[:]
}
func (n *Node) Call(target, method string, data interface{}, out interface{}) error {
	raw, e := json.Marshal(data)
	if e != nil {
		return e
	}
	env := &Envelope{ID: rid(16), Origin: n.id, Pub: n.pub, Time: time.Now().Unix(), Target: target, Method: method, Data: raw}
	env.Signature, e = ecdsa.SignASN1(rand.Reader, n.key, signatureBody(env))
	if e != nil {
		return e
	}
	result, e := n.forward(env)
	if e != nil {
		return e
	}
	return json.Unmarshal(result, out)
}
func (n *Node) forward(e *Envelope) (json.RawMessage, error) {
	if e.Binary != nil && e.Target != n.id {
		return nil, errors.New("二进制块不经中转，请使用兼容路径")
	}
	if len(e.Visited) > 7 || contains(e.Visited, n.id) {
		return nil, errors.New("中转路径循环或过长")
	}
	if identity(e.Pub) != e.Origin {
		return nil, errors.New("发送方身份无效")
	}
	pub, err := x509.ParsePKIXPublicKey(e.Pub)
	if err != nil {
		return nil, err
	}
	key, ok := pub.(*ecdsa.PublicKey)
	if !ok || !ecdsa.VerifyASN1(key, signatureBody(e), e.Signature) {
		return nil, errors.New("请求签名无效")
	}
	if delta := time.Now().Unix() - e.Time; delta > 300 || delta < -300 {
		return nil, errors.New("设备时间相差过大，请校准时间")
	}
	if e.Target == n.id || strings.HasPrefix(e.Target, "web:"+n.id+":") {
		return n.handle(e)
	}
	n.mu.Lock()
	type choice struct {
		l    *Link
		cost int
	}
	var choices []choice
	for _, l := range n.links {
		if contains(e.Visited, l.peer.ID) {
			continue
		}
		if l.peer.ID == e.Target {
			choices = append(choices, choice{l, 0})
			continue
		}
		for _, r := range l.routes {
			if r.ID == e.Target && !contains(r.Path, n.id) {
				bad := false
				for _, id := range e.Visited {
					if contains(r.Path, id) {
						bad = true
					}
				}
				if !bad {
					choices = append(choices, choice{l, len(r.Path)})
				}
				break
			}
		}
	}
	n.mu.Unlock()
	if len(choices) == 0 {
		return nil, errors.New("目标设备当前没有可连接路径")
	}
	for i := 0; i < len(choices); i++ {
		for j := i + 1; j < len(choices); j++ {
			if choices[j].cost < choices[i].cost {
				choices[i], choices[j] = choices[j], choices[i]
			}
		}
	}
	copyE := *e
	copyE.Visited = append(append([]string{}, e.Visited...), n.id)
	var last error
	for _, c := range choices {
		raw, err := c.l.call(&copyE)
		if err == nil {
			return raw, nil
		}
		last = err
	}
	return nil, last
}
