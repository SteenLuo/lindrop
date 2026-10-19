package main

import (
	"archive/zip"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var web embed.FS

func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func jsonOut(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func apiError(w http.ResponseWriter, status int, e interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	jsonOut(w, map[string]interface{}{"error": fmt.Sprint(e)})
}
func input(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		apiError(w, 415, "请求格式无效")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*1024*1024))
	if e := d.Decode(v); e != nil {
		apiError(w, 400, e)
		return false
	}
	return true
}
func (n *Node) isAdmin(r *http.Request) bool {
	h, _, _ := net.SplitHostPort(r.RemoteAddr)
	return net.ParseIP(h).IsLoopback() && equal(r.Header.Get("X-Admin-Key"), n.admin)
}
func (n *Node) webClient(r *http.Request) (WebClient, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	c, ok := n.browserClientLocked(r)
	if ok {
		n.webSeen[c.Key] = time.Now()
	}
	return c, ok
}
func (n *Node) browserCookieName() string { return "linnode_client_" + n.id }
func (n *Node) browserClientLocked(r *http.Request) (WebClient, bool) {
	for _, name := range []string{n.browserCookieName(), "linnode_client"} {
		if cookie, e := r.Cookie(name); e == nil {
			if c, ok := n.cfg.Clients[cookie.Value]; ok {
				return c, true
			}
		}
	}
	return WebClient{}, false
}
func (n *Node) handler() http.Handler {
	files, _ := fs.Sub(web, "web")
	static := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		p := net.ParseIP(ip)
		if p == nil || (!p.IsPrivate() && !p.IsLoopback()) {
			http.Error(w, "仅限局域网", 403)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		if r.Host != n.addr && r.Host != n.localAddr {
			apiError(w, 403, "请使用客户端显示的地址")
			return
		}
		if r.URL.Path == "/node/link" && r.Header.Get("Upgrade") == "linnode" {
			n.acceptLink(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
				apiError(w, 403, "拒绝跨站请求")
				return
			}
			if r.Method != "GET" && r.Method != "POST" {
				apiError(w, 405, "不支持该操作")
				return
			}
			if r.URL.Path == "/api/pair" && r.Method == "POST" {
				n.pairWeb(w, r)
				return
			}
			admin := n.isAdmin(r)
			client, auth := n.webClient(r)
			if !auth && !admin {
				apiError(w, 401, "请先扫码或输入配对码")
				return
			}
			n.webAPI(w, r, admin, client)
			return
		}
		if r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/", "/app.js", "/style.css", "/qrcode.js", "/sha256.js":
			static.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
func (n *Node) Start(ip string, port int) error {
	if ip == "" {
		n.mu.Lock()
		ip = n.cfg.IP
		n.mu.Unlock()
		if ip != "" {
			if test, e := net.Listen("tcp4", net.JoinHostPort(ip, "0")); e == nil {
				test.Close()
			} else {
				ip = ""
			}
		}
	}
	if ip == "" {
		nets := enumerateNetworks()
		if len(nets) == 0 {
			return errors.New("没有找到可用的局域网地址")
		}
		ip = nets[0].IP
	}
	if !privateAddr(net.JoinHostPort(ip, "1")) {
		return errors.New("局域网地址无效")
	}
	ln, e := net.Listen("tcp4", net.JoinHostPort(ip, strconv.Itoa(port)))
	if e != nil && port != 0 {
		ln, e = net.Listen("tcp4", net.JoinHostPort(ip, "0"))
	}
	if e != nil {
		return e
	}
	actual := ln.Addr().(*net.TCPAddr).Port
	n.addr = net.JoinHostPort(ip, strconv.Itoa(actual))
	n.localAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(actual))
	listeners := []net.Listener{ln}
	// Loopback-only runs already own the local endpoint. Opening it a second
	// time makes the process conflict with its own listener on Windows.
	if n.addr != n.localAddr {
		loop, err := net.Listen("tcp4", n.localAddr)
		if err != nil {
			ln.Close()
			return fmt.Errorf("无法启用本机入口（端口 %d），请重试或更换端口：%w", actual, err)
		}
		listeners = append(listeners, loop)
	}
	n.mu.Lock()
	n.cfg.IP = ip
	n.mu.Unlock()
	for _, listener := range listeners {
		s := &http.Server{Handler: n.handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 40 * time.Second, MaxHeaderBytes: 16384}
		n.httpServers = append(n.httpServers, s)
		go s.Serve(listener)
	}
	n.persist()
	n.autoAcceptPendingTexts()
	n.wg.Add(1)
	go n.background()
	go n.discover(ip)
	return nil
}
func (n *Node) background() {
	defer n.wg.Done()
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-n.stop:
			return
		case <-tick.C:
			n.announce()
			n.kick()
			n.persist()
			n.mu.Lock()
			peers := map[string]Peer{}
			for id, p := range n.cfg.Peers {
				peers[id] = p
			}
			for id, p := range n.nearby {
				peers[id] = p
			}
			n.mu.Unlock()
			for _, p := range peers {
				n.connectNearby(p)
			}
		}
	}
}

type beacon struct {
	Magic string `json:"magic"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Addr  string `json:"addr"`
}

func (n *Node) discover(ip string) {
	conn, e := net.ListenUDP("udp4", &net.UDPAddr{Port: 47766})
	if e != nil {
		return
	}
	defer conn.Close()
	go func() { <-n.stop; conn.Close() }()
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	go func() {
		send := func() {
			raw, _ := json.Marshal(beacon{"linnode-1", n.id, n.name(), n.addr})
			conn.WriteToUDP(raw, &net.UDPAddr{IP: net.IPv4bcast, Port: 47766})
			ifaces, _ := net.Interfaces()
			for _, it := range ifaces {
				if it.Flags&net.FlagUp == 0 {
					continue
				}
				addrs, _ := it.Addrs()
				for _, addr := range addrs {
					v, network, e := net.ParseCIDR(addr.String())
					if e != nil || v.To4() == nil || !v.IsPrivate() {
						continue
					}
					v = v.To4()
					mask := network.Mask
					broadcast := net.IPv4(v[0]|^mask[0], v[1]|^mask[1], v[2]|^mask[2], v[3]|^mask[3])
					conn.WriteToUDP(raw, &net.UDPAddr{IP: broadcast, Port: 47766})
				}
			}
		}
		send()
		for {
			select {
			case <-n.stop:
				return
			case <-ticker.C:
				send()
			}
		}
	}()
	buf := make([]byte, 4096)
	for {
		size, remote, e := conn.ReadFromUDP(buf)
		if e != nil {
			return
		}
		n.observeBeacon(buf[:size], remote.IP)
	}
}

// Source and certificate must both match the advertised identity. No pairing UI.
func (n *Node) observeBeacon(raw []byte, remote net.IP) bool {
	if !remote.IsPrivate() && !remote.IsLoopback() {
		return false
	}
	var b beacon
	if json.Unmarshal(raw, &b) != nil || b.Magic != "linnode-1" || len(b.ID) != 32 || b.ID == n.id || !privateAddr(b.Addr) {
		return false
	}
	host, _, _ := net.SplitHostPort(b.Addr)
	if host != remote.String() {
		return false
	}
	p := Peer{b.ID, b.Name, b.Addr}
	n.mu.Lock()
	n.nearby[b.ID] = p
	if _, ok := n.cfg.Peers[b.ID]; ok {
		n.cfg.Peers[b.ID] = p
	}
	n.mu.Unlock()
	n.connectNearby(p)
	return true
}
func (n *Node) connectNearby(p Peer) {
	n.mu.Lock()
	if p.ID == n.id || n.links[p.ID] != nil || n.connecting[p.ID] || p.Addr == "" {
		n.mu.Unlock()
		return
	}
	n.connecting[p.ID] = true
	n.mu.Unlock()
	go func() {
		defer func() { n.mu.Lock(); delete(n.connecting, p.ID); n.mu.Unlock() }()
		select {
		case <-n.stop:
			return
		default:
		}
		_ = n.dialExpected(p.Addr, "", p.ID)
	}()
}

func (n *Node) pairWeb(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Code   string `json:"code"`
		Join   string `json:"join"`
		Key    string `json:"key"`
		Name   string `json:"name"`
		Rename bool   `json:"rename"`
	}
	if !input(w, r, &v) {
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	n.mu.Lock()
	if time.Since(n.attemptTime) > time.Minute {
		n.codeAttempts = map[string]int{}
		n.attemptTime = time.Now()
	}
	c, known := n.cfg.Clients[v.Key]
	// Cookies survive a changed HTTP port on the same host. Only a cookie
	// already issued by this node may restore its own browser identity.
	if !known {
		c, known = n.browserClientLocked(r)
	}
	valid := known || (n.codeAttempts[ip] < 5 && ((len(v.Code) == 6 && equal(v.Code, n.code)) || (v.Join != "" && equal(v.Join, n.webToken))))
	if !valid {
		n.codeAttempts[ip]++
		n.mu.Unlock()
		apiError(w, 401, "配对码错误或尝试次数过多，请一分钟后重试")
		return
	}
	if !known {
		c = WebClient{ID: rid(12), Name: "我的手机", Key: rid(24)}
	}
	name := strings.TrimSpace(v.Name)
	if v.Rename && (name == "" || len([]rune(name)) > 30) {
		n.mu.Unlock()
		apiError(w, 400, "设备名称需为1至30个字")
		return
	}
	if name != "" && len([]rune(name)) <= 30 && (!known || v.Rename || v.Join == "" && v.Code == "") {
		c.Name = name
	}
	n.cfg.Clients[c.Key] = c
	n.webSeen[c.Key] = time.Now()
	n.mu.Unlock()
	n.persist()
	http.SetCookie(w, &http.Cookie{Name: n.browserCookieName(), Value: c.Key, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 365 * 86400})
	jsonOut(w, map[string]interface{}{"id": c.ID, "key": c.Key, "name": c.Name, "created": !known, "entryName": n.name()})
	n.announce()
}
func (n *Node) snapshot(admin bool, c WebClient) map[string]interface{} {
	devices := n.devices()
	n.mu.Lock()
	defer n.mu.Unlock()
	tasks := []Task{}
	sid := "web:" + n.id + ":" + c.ID
	for _, t := range n.cfg.Tasks {
		if !admin && t.Client != c.ID && t.Target != sid {
			continue
		}
		copyT := *t
		copyT.Media = taskMediaKind(*t)
		copyT.Files = nil
		if !admin && copyT.Direction == "out" && copyT.Owner == n.id && copyT.Target == sid {
			copyT.Direction = "web"
		}
		if !admin {
			copyT.Root = ""
		}
		tasks = append(tasks, copyT)
	}
	sort.Slice(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if a.Created != b.Created {
			return a.Created > b.Created
		}
		if a.CreatedNS != b.CreatedNS {
			return a.CreatedNS > b.CreatedNS
		}
		return a.ID < b.ID
	})
	out := map[string]interface{}{"id": n.id, "name": n.cfg.Name, "devices": devices, "tasks": tasks, "client": c, "admin": admin}
	if admin {
		out["saveDir"] = n.cfg.SaveDir
		out["code"] = n.code
		out["url"] = "http://" + n.addr + "/"
		out["joinURL"] = "http://" + n.addr + "/#join=" + n.webToken
	}
	return out
}
func (n *Node) webAPI(w http.ResponseWriter, r *http.Request, admin bool, c WebClient) {
	if strings.HasPrefix(r.URL.Path, "/api/admin/") && r.URL.Path != "/api/admin/action" {
		if !admin || r.Method != "POST" {
			apiError(w, 403, "仅本机客户端可操作")
			return
		}
		var v struct {
			Addr, Code, Name, Target, ID, Action, Text string
			Paths                                      []string
			Trust                                      bool
		}
		if !input(w, r, &v) {
			return
		}
		var err error
		var result interface{} = map[string]bool{"ok": true}
		switch strings.TrimPrefix(r.URL.Path, "/api/admin/") {
		case "pair":
			err = n.Dial(v.Addr, v.Code)
		case "send":
			var id string
			id, err = n.Send(v.Paths, v.Target)
			result = map[string]string{"id": id}
		case "text":
			var id string
			id, err = n.SendText(v.Text, v.Target)
			result = map[string]string{"id": id}
		case "share":
			err = n.AddShare(v.Paths)
		case "name":
			err = n.SetName(v.Name)
		case "accept":
			err = n.Accept(v.ID, v.Trust)
		case "shutdown":
			jsonOut(w, result)
			go n.Close()
			return
		default:
			apiError(w, 404, "操作不存在")
			return
		}
		if err != nil {
			apiError(w, 400, err)
			return
		}
		jsonOut(w, result)
		return
	}
	switch r.URL.Path {
	case "/api/phone-names":
		if r.Method != "GET" {
			apiError(w, 405, "请使用读取请求")
			return
		}
		jsonOut(w, n.availablePhoneNames())
	case "/api/phone-name":
		if r.Method != "POST" || admin {
			apiError(w, 403, "请在手机页面选择自己的名称")
			return
		}
		var v struct {
			Ref string `json:"ref"`
		}
		if !input(w, r, &v) {
			return
		}
		name, e := n.usePhoneName(c, v.Ref)
		if e != nil {
			apiError(w, 400, e)
			return
		}
		jsonOut(w, map[string]string{"name": name})
	case "/api/text":
		if r.Method != "GET" {
			apiError(w, 405, "请使用读取请求")
			return
		}
		s, e := n.readWebText(r.URL.Query().Get("id"), c, admin)
		if e != nil {
			apiError(w, 403, e)
			return
		}
		jsonOut(w, map[string]string{"text": s})
	case "/api/state":
		jsonOut(w, n.snapshot(admin, c))
	case "/api/devices":
		jsonOut(w, n.devices())
	case "/api/shares":
		var shares []Share
		if e := n.Call(r.URL.Query().Get("target"), "shares", struct{}{}, &shares); e != nil {
			apiError(w, 400, e)
			return
		}
		jsonOut(w, shares)
	case "/api/media":
		n.serveMedia(w, r, c, admin)
	case "/api/download":
		n.downloadWeb(w, r, c)
	case "/api/task/offer":
		if r.Method != "POST" {
			apiError(w, 405, "请提交任务")
			return
		}
		var v struct {
			Target string `json:"target"`
			Offer  Offer  `json:"offer"`
		}
		if !input(w, r, &v) {
			return
		}
		if admin {
			apiError(w, 400, "请使用客户端发送")
			return
		}
		v.Offer.Sender = "web:" + n.id + ":" + c.ID
		if strings.HasPrefix(v.Target, "web:") {
			apiError(w, 400, "网页端请选择电脑节点作为发送目标")
			return
		}
		v.Offer.SenderName = c.Name
		if v.Offer.ID == "" {
			v.Offer.ID = rid(16)
		}
		n.mu.Lock()
		old := n.cfg.Tasks[v.Offer.ID]
		if old != nil && old.Client != c.ID {
			n.mu.Unlock()
			apiError(w, 403, "无权访问此任务")
			return
		}
		n.mu.Unlock()
		var reply TaskReply
		if e := n.Call(v.Target, "offer", v.Offer, &reply); e != nil {
			apiError(w, 400, e)
			return
		}
		targetName := v.Target
		for _, d := range n.devices() {
			if d.ID == v.Target {
				targetName = d.Name
			}
		}
		n.mu.Lock()
		if old == nil && v.Target != n.id {
			t := &Task{Kind: v.Offer.Kind, ID: v.Offer.ID, Direction: "webout", Client: c.ID, Owner: n.id, Sender: v.Offer.Sender, SenderName: c.Name, Target: v.Target, TargetName: targetName, Title: v.Offer.Title, Files: cleanFiles(v.Offer.Files), Status: reply.Status, Created: time.Now().Unix(), CreatedNS: time.Now().UnixNano()}
			for _, f := range t.Files {
				t.Total += f.Size
			}
			n.cfg.Tasks[t.ID] = t
		} else if old == nil {
			n.cfg.Tasks[v.Offer.ID].Client = c.ID
		}
		n.mu.Unlock()
		jsonOut(w, map[string]interface{}{"id": v.Offer.ID, "reply": reply})
	case "/api/task/chunk":
		if r.Method != "POST" || r.Header.Get("X-LinNode-Upload") != "1" {
			apiError(w, 400, "上传请求无效")
			return
		}
		id := r.URL.Query().Get("id")
		n.mu.Lock()
		t := n.cfg.Tasks[id]
		owned := t != nil && t.Client == c.ID
		target := ""
		if owned {
			target = t.Target
		}
		n.mu.Unlock()
		if !owned {
			apiError(w, 403, "无权访问此任务")
			return
		}
		index, _ := strconv.Atoi(r.URL.Query().Get("index"))
		offset, e := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		if e != nil {
			apiError(w, 400, "偏移错误")
			return
		}
		data, e := io.ReadAll(http.MaxBytesReader(w, r.Body, chunkSize))
		if e != nil {
			apiError(w, 413, e)
			return
		}
		var ack struct {
			Offset int64 `json:"offset"`
		}
		if e = n.Call(target, "chunk", Chunk{id, index, offset, data}, &ack); e != nil {
			apiError(w, 400, e)
			return
		}
		if target != n.id {
			n.mu.Lock()
			if index >= 0 && index < len(t.Files) {
				t.Done += ack.Offset - t.Files[index].Offset
				t.Files[index].Offset = ack.Offset
			}
			t.Status = "sending"
			n.mu.Unlock()
		}
		jsonOut(w, ack)
	case "/api/task/finish":
		var v struct {
			ID string `json:"id"`
		}
		if r.Method != "POST" || !input(w, r, &v) {
			return
		}
		n.mu.Lock()
		t := n.cfg.Tasks[v.ID]
		owned := t != nil && t.Client == c.ID
		target := ""
		if owned {
			target = t.Target
		}
		n.mu.Unlock()
		if !owned {
			apiError(w, 403, "无权访问此任务")
			return
		}
		var reply TaskReply
		if e := n.Call(target, "finish", struct{ ID string }{v.ID}, &reply); e != nil {
			apiError(w, 400, e)
			return
		}
		n.mu.Lock()
		t.Status = reply.Status
		t.Done = t.Total
		n.mu.Unlock()
		jsonOut(w, reply)
	case "/api/admin/action":
		if !admin || r.Method != "POST" {
			apiError(w, 403, "仅本机客户端可操作")
			return
		}
		var v struct{ ID, Action string }
		if !input(w, r, &v) {
			return
		}
		if e := n.TaskAction(v.ID, v.Action); e != nil {
			apiError(w, 400, e)
			return
		}
		jsonOut(w, map[string]bool{"ok": true})
	default:
		apiError(w, 404, "操作不存在")
	}
}
func (n *Node) downloadWeb(w http.ResponseWriter, r *http.Request, c WebClient) {
	q := r.URL.Query()
	target, shareID := q.Get("target"), q.Get("share")
	var share Share
	method := "share.chunk"
	taskID := q.Get("inbox")
	if taskID != "" {
		n.mu.Lock()
		t := n.cfg.Tasks[taskID]
		sid := "web:" + n.id + ":" + c.ID
		if t == nil || (t.Direction != "web" && !(t.Direction == "out" && t.Owner == n.id && t.Sender == n.id && t.Status != "preparing")) || t.Target != sid {
			n.mu.Unlock()
			apiError(w, 403, "无权下载此任务")
			return
		}
		share = Share{ID: t.ID, Name: t.Title, Folder: t.Folder, Files: cleanFiles(t.Files)}
		target = t.Owner
		method = "out.read"
		n.mu.Unlock()
	} else {
		if e := n.Call(target, "share.manifest", ShareRequest{ID: shareID}, &share); e != nil {
			apiError(w, 400, e)
			return
		}
	}
	if len(share.Files) == 0 {
		apiError(w, 404, "文件不存在")
		return
	}
	filename := share.Name
	if share.Folder || len(share.Files) > 1 {
		filename += ".zip"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	if q.Get("preview") == "1" && taskID != "" && !share.Folder && len(share.Files) == 1 && (mediaKind(share.Name, false, 1) == "image" || mediaKind(share.Name, false, 1) == "video") {
		w.Header().Set("Content-Type", mediaContentType(share.Name))
		w.Header().Set("Content-Disposition", "inline")
	}
	fetch := func(dst io.Writer, index int, start, end int64) error {
		for offset := start; offset < end; {
			count := int64(chunkSize)
			if count > end-offset {
				count = end - offset
			}
			var v struct {
				Data []byte `json:"data"`
			}
			if e := n.Call(target, method, ShareRequest{ID: share.ID, Index: index, Offset: offset, Length: int(count)}, &v); e != nil {
				return e
			}
			if len(v.Data) == 0 {
				return io.ErrUnexpectedEOF
			}
			if _, e := dst.Write(v.Data); e != nil {
				return e
			}
			offset += int64(len(v.Data))
			select {
			case <-r.Context().Done():
				return r.Context().Err()
			default:
			}
		}
		return nil
	}
	var err error
	if share.Folder || len(share.Files) > 1 {
		zw := zip.NewWriter(w)
		for i, f := range share.Files {
			header := &zip.FileHeader{Name: f.Rel, Method: zip.Store}
			var dst io.Writer
			dst, err = zw.CreateHeader(header)
			if err != nil {
				break
			}
			if err = fetch(dst, i, 0, f.Size); err != nil {
				break
			}
		}
		if err == nil {
			err = zw.Close()
		}
	} else {
		f := share.Files[0]
		start, end := int64(0), f.Size
		w.Header().Set("Accept-Ranges", "bytes")
		if rg := r.Header.Get("Range"); rg != "" {
			if _, e := fmt.Sscanf(rg, "bytes=%d-", &start); e != nil || start < 0 || start >= f.Size {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", f.Size))
				w.WriteHeader(416)
				return
			}
			parts := strings.Split(strings.TrimPrefix(rg, "bytes="), "-")
			if len(parts) == 2 && parts[1] != "" {
				if last, e := strconv.ParseInt(parts[1], 10, 64); e == nil && last >= start && last < f.Size {
					end = last + 1
				}
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, f.Size))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start, 10))
			w.WriteHeader(206)
		} else {
			w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
		}
		err = fetch(w, 0, start, end)
	}
	if err == nil && taskID != "" && q.Get("preview") != "1" {
		n.mu.Lock()
		if t := n.cfg.Tasks[taskID]; t != nil {
			t.Status = "done"
			t.Error = ""
			t.Done = t.Total
		}
		n.mu.Unlock()
	}
	if err == nil && taskID != "" && q.Get("preview") != "1" {
		n.persist()
	}
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}
func (n *Node) SetName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 30 {
		return errors.New("设备名称需为 1 至 30 个字")
	}
	n.mu.Lock()
	n.cfg.Name = name
	n.mu.Unlock()
	n.announce()
	return n.persist()
}
func (n *Node) SetFolder(path string) error {
	f, e := os.CreateTemp(path, ".linnode-check-")
	if e != nil {
		return e
	}
	f.Close()
	os.Remove(f.Name())
	n.mu.Lock()
	n.cfg.SaveDir = path
	n.mu.Unlock()
	return n.persist()
}
func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, u := range units {
		v /= 1024
		if v < 1024 {
			return fmt.Sprintf("%.1f %s", v, u)
		}
	}
	return fmt.Sprintf("%.1f TB", v)
}
