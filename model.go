package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const chunkSize = 256 * 1024
const maxFileSize int64 = 100 * 1024 * 1024 * 1024

type Peer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Addr string `json:"addr"`
}
type Meta struct {
	Rel    string `json:"rel"`
	Size   int64  `json:"size"`
	SHA    string `json:"sha"`
	Source string `json:"source,omitempty"`
	Mod    int64  `json:"mod,omitempty"`
	Temp   string `json:"temp,omitempty"`
	Final  string `json:"final,omitempty"`
	Offset int64  `json:"offset"`
}
type Task struct {
	Media      string  `json:"media,omitempty"`
	CreatedNS  int64   `json:"createdOrder,omitempty"`
	Kind       string  `json:"kind,omitempty"`
	ID         string  `json:"id"`
	Direction  string  `json:"direction"`
	Owner      string  `json:"owner"`
	Sender     string  `json:"sender"`
	SenderName string  `json:"senderName"`
	Target     string  `json:"target"`
	TargetName string  `json:"targetName"`
	Title      string  `json:"title"`
	Folder     bool    `json:"folder"`
	Files      []Meta  `json:"files"`
	Status     string  `json:"status"`
	Error      string  `json:"error"`
	Total      int64   `json:"total"`
	Done       int64   `json:"done"`
	Rate       float64 `json:"rate"`
	Created    int64   `json:"created"`
	Root       string  `json:"root,omitempty"`
	Manual     bool    `json:"manual"`
	Client     string  `json:"client,omitempty"`
	Pull       bool    `json:"pull,omitempty"`
	ShareID    string  `json:"shareID,omitempty"`
}
type Share struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Folder bool   `json:"folder"`
	Files  []Meta `json:"files"`
	Size   int64  `json:"size"`
}
type WebClient struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
}
type Config struct {
	Notifications *NoticeSettings      `json:"notifications,omitempty"`
	Key           string               `json:"key"`
	Name          string               `json:"name"`
	SaveDir       string               `json:"saveDir"`
	IP            string               `json:"ip"`
	Peers         map[string]Peer      `json:"peers"`
	Trust         map[string]bool      `json:"trust"`
	Clients       map[string]WebClient `json:"clients"`
	Tasks         map[string]*Task     `json:"tasks"`
	Shares        map[string]*Share    `json:"shares"`
}
type Route struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Path []string `json:"path"`
}
type Device struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Addr   string `json:"addr"`
	Online bool   `json:"online"`
	Paired bool   `json:"paired"`
	Via    string `json:"via"`
}
type Node struct {
	mu                                              sync.Mutex
	saveMu                                          sync.Mutex
	cfg                                             Config
	dir, id, admin, code, addr, localAddr, webToken string
	key                                             *ecdsa.PrivateKey
	pub                                             []byte
	cert                                            tls.Certificate
	links                                           map[string]*Link
	nearby                                          map[string]Peer
	busy                                            map[string]bool
	locks                                           map[string]*sync.Mutex
	webSeen                                         map[string]time.Time
	httpServers                                     []interface{ Close() error }
	stop                                            chan struct{}
	once                                            sync.Once
	wg                                              sync.WaitGroup
	picker                                          sync.Mutex
	connecting                                      map[string]bool
	codeAttempts                                    map[string]int
	attemptTime                                     time.Time
	phoneNamesMu                                    sync.Mutex
	phoneNamesCache                                 []PhoneName
	phoneNamesUntil                                 time.Time
}

func rid(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func sixCode() string {
	v, e := rand.Int(rand.Reader, big.NewInt(1000000))
	if e != nil {
		panic(e)
	}
	return fmt.Sprintf("%06d", v.Int64())
}
func identity(pub []byte) string { h := sha256.Sum256(pub); return hex.EncodeToString(h[:16]) }
func NewNode(dir, name, save string) (*Node, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	n := &Node{dir: dir, admin: rid(32), code: sixCode(), webToken: rid(24), links: map[string]*Link{}, nearby: map[string]Peer{}, busy: map[string]bool{}, locks: map[string]*sync.Mutex{}, webSeen: map[string]time.Time{}, stop: make(chan struct{}), connecting: map[string]bool{}, codeAttempts: map[string]int{}, attemptTime: time.Now()}
	raw, e := os.ReadFile(filepath.Join(dir, "settings.json"))
	if e == nil {
		if e = json.Unmarshal(raw, &n.cfg); e != nil {
			return nil, fmt.Errorf("配置损坏，请保留 settings.json 后联系修复: %w", e)
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if n.cfg.Key == "" {
		n.key, e = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return nil, e
		}
		der, _ := x509.MarshalECPrivateKey(n.key)
		n.cfg.Key = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	} else {
		block, _ := pem.Decode([]byte(n.cfg.Key))
		if block == nil {
			return nil, errors.New("设备密钥损坏")
		}
		n.key, e = x509.ParseECPrivateKey(block.Bytes)
		if e != nil {
			return nil, e
		}
	}
	n.pub, _ = x509.MarshalPKIXPublicKey(&n.key.PublicKey)
	n.id = identity(n.pub)
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: n.id}, NotBefore: time.Now().Add(-24 * time.Hour), NotAfter: time.Now().AddDate(20, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &n.key.PublicKey, n.key)
	if e != nil {
		return nil, e
	}
	n.cert = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: n.key}
	if n.cfg.Name == "" {
		n.cfg.Name, _ = os.Hostname()
	}
	if name != "" {
		n.cfg.Name = name
	}
	if save != "" {
		n.cfg.SaveDir = save
	}
	if n.cfg.SaveDir == "" {
		home, _ := os.UserHomeDir()
		n.cfg.SaveDir = filepath.Join(home, "Downloads", "LinDrop")
	}
	if e = os.MkdirAll(n.cfg.SaveDir, 0700); e != nil {
		return nil, e
	}
	if n.cfg.Peers == nil {
		n.cfg.Peers = map[string]Peer{}
	}
	if n.cfg.Trust == nil {
		n.cfg.Trust = map[string]bool{}
	}
	if n.cfg.Clients == nil {
		n.cfg.Clients = map[string]WebClient{}
	}
	if n.cfg.Tasks == nil {
		n.cfg.Tasks = map[string]*Task{}
	}
	if n.cfg.Shares == nil {
		n.cfg.Shares = map[string]*Share{}
	}
	for _, t := range n.cfg.Tasks {
		if t.Direction == "in" && t.Status != "done" {
			t.Done = 0
			for i := range t.Files {
				f := &t.Files[i]
				if f.Temp != "" {
					if st, e := os.Stat(f.Temp); e == nil && st.Size() <= f.Size {
						f.Offset = st.Size()
					} else {
						f.Offset = 0
					}
				}
				if f.Final != "" {
					if st, e := os.Stat(f.Final); e == nil && st.Size() == f.Size {
						f.Offset = f.Size
					}
				}
				t.Done += f.Offset
			}
		} else if t.Direction == "out" && t.Status != "done" && t.Status != "rejected" {
			t.Status = "paused"
		}
	}
	return n, n.persist()
}
func (n *Node) persist() error {
	n.saveMu.Lock()
	defer n.saveMu.Unlock()
	n.mu.Lock()
	raw, e := json.MarshalIndent(n.cfg, "", " ")
	n.mu.Unlock()
	if e != nil {
		return e
	}
	tmp := filepath.Join(n.dir, "settings.tmp")
	if e = os.WriteFile(tmp, raw, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, filepath.Join(n.dir, "settings.json"))
}
func (n *Node) name() string { n.mu.Lock(); defer n.mu.Unlock(); return n.cfg.Name }
func (n *Node) taskLock(id string) *sync.Mutex {
	n.mu.Lock()
	defer n.mu.Unlock()
	m := n.locks[id]
	if m == nil {
		m = &sync.Mutex{}
		n.locks[id] = m
	}
	return m
}
func (n *Node) Close() {
	n.once.Do(func() {
		close(n.stop)
		for _, s := range n.httpServers {
			s.Close()
		}
		n.mu.Lock()
		var links []*Link
		for _, l := range n.links {
			links = append(links, l)
		}
		n.mu.Unlock()
		for _, l := range links {
			l.close()
		}
		n.wg.Wait()
		n.persist()
	})
}
func (n *Node) routesLocked() map[string]Route {
	out := map[string]Route{n.id: {ID: n.id, Name: n.cfg.Name, Path: []string{n.id}}}
	for key, seen := range n.webSeen {
		if time.Since(seen) < 20*time.Second {
			c := n.cfg.Clients[key]
			id := "web:" + n.id + ":" + c.ID
			out[id] = Route{id, c.Name, []string{n.id, id}}
		}
	}
	for _, l := range n.links {
		for _, r := range l.routes {
			if r.ID == n.id || contains(r.Path, n.id) || len(r.Path) > 6 {
				continue
			}
			p := append([]string{n.id}, r.Path...)
			old, ok := out[r.ID]
			if !ok || len(p) < len(old.Path) {
				out[r.ID] = Route{r.ID, r.Name, p}
			}
		}
		if _, ok := out[l.peer.ID]; !ok {
			out[l.peer.ID] = Route{l.peer.ID, l.peer.Name, []string{n.id, l.peer.ID}}
		}
	}
	return out
}
func contains(v []string, x string) bool {
	for _, s := range v {
		if s == x {
			return true
		}
	}
	return false
}
func (n *Node) devices() []Device {
	n.mu.Lock()
	defer n.mu.Unlock()
	routes := n.routesLocked()
	all := map[string]Device{}
	for id, p := range n.nearby {
		if id != n.id {
			all[id] = Device{ID: id, Name: p.Name, Addr: p.Addr}
		}
	}
	for id, p := range n.cfg.Peers {
		all[id] = Device{ID: id, Name: p.Name, Addr: p.Addr, Paired: true}
	}
	for id, r := range routes {
		if id == n.id {
			continue
		}
		d := all[id]
		d.ID = id
		d.Name = r.Name
		d.Online = true
		d.Paired = true
		if len(r.Path) > 2 {
			via := routes[r.Path[1]].Name
			if via == "" {
				via = r.Path[1]
			}
			d.Via = via
		}
		all[id] = d
	}
	out := []Device{{ID: n.id, Name: n.cfg.Name + "（本机）", Online: true, Paired: true, Addr: n.addr}}
	for _, v := range all {
		out = append(out, v)
	}
	sort.Slice(out[1:], func(i, j int) bool {
		a, b := out[i+1], out[j+1]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	return out
}
func shaFile(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.CopyBuffer(h, f, make([]byte, chunkSize)); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func safePart(raw string) (string, error) {
	if raw == "" || raw == "." || raw == ".." || strings.ContainsAny(raw, "/\\\x00") {
		return "", errors.New("文件名无效")
	}
	var b strings.Builder
	for _, c := range raw {
		if c < 32 || strings.ContainsRune("<>:\"|?*", c) {
			b.WriteRune('_')
		} else {
			b.WriteRune(c)
		}
	}
	s := strings.TrimRight(b.String(), ". ")
	if s == "" || len([]rune(s)) > 150 {
		return "", errors.New("文件名为空或过长")
	}
	stem := strings.ToUpper(strings.Split(s, ".")[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT"))) {
		s = "_" + s
	}
	return s, nil
}
func safeRel(raw string) (string, error) {
	if strings.Contains(raw, "\\") || strings.HasPrefix(raw, "/") {
		return "", errors.New("非法相对路径")
	}
	parts := strings.Split(raw, "/")
	if len(parts) > 30 {
		return "", errors.New("目录过深")
	}
	for i, s := range parts {
		v, e := safePart(s)
		if e != nil {
			return "", e
		}
		parts[i] = v
	}
	return filepath.Join(parts...), nil
}
func collect(paths []string) ([]Meta, bool, string, error) {
	var files []Meta
	folder := len(paths) == 1
	title := "多个文件"
	for _, p := range paths {
		st, e := os.Stat(p)
		if e != nil {
			return nil, false, "", e
		}
		if len(paths) == 1 {
			title = st.Name()
			folder = st.IsDir()
		}
		if st.IsDir() {
			e = filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.Mode()&os.ModeSymlink != 0 {
					return nil
				}
				if info.IsDir() {
					return nil
				}
				if !info.Mode().IsRegular() {
					return nil
				}
				rel, e := filepath.Rel(p, path)
				if e != nil {
					return e
				}
				files = append(files, Meta{Rel: filepath.ToSlash(rel), Source: path, Size: info.Size(), Mod: info.ModTime().UnixNano()})
				if len(files) > 5000 {
					return errors.New("一次最多 5000 个文件")
				}
				return nil
			})
			if e != nil {
				return nil, false, "", e
			}
		} else if st.Mode().IsRegular() {
			files = append(files, Meta{Rel: st.Name(), Source: p, Size: st.Size(), Mod: st.ModTime().UnixNano()})
		} else {
			return nil, false, "", errors.New("仅支持普通文件与文件夹")
		}
	}
	if len(files) == 0 {
		return nil, false, "", errors.New("没有可传输的文件（空文件夹暂不传输）")
	}
	for _, f := range files {
		if f.Size > maxFileSize {
			return nil, false, "", errors.New("单文件不能超过 100 GiB")
		}
	}
	return files, folder, title, nil
}
func privateAddr(addr string) bool {
	h, _, e := net.SplitHostPort(addr)
	return e == nil && net.ParseIP(h) != nil && (net.ParseIP(h).IsPrivate() || net.ParseIP(h).IsLoopback())
}

type network struct {
	IP, Name string
	Score    int
}

func enumerateNetworks() []network {
	var out []network
	ifs, _ := net.Interfaces()
	for _, it := range ifs {
		if it.Flags&net.FlagUp == 0 || it.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := it.Addrs()
		for _, v := range addrs {
			ip, _, e := net.ParseCIDR(v.String())
			if e != nil || ip.To4() == nil || !ip.IsPrivate() {
				continue
			}
			score := 10
			name := strings.ToLower(it.Name)
			if strings.Contains(name, "wlan") || strings.Contains(name, "wi-fi") || strings.Contains(name, "无线") {
				score = 100
			}
			if strings.Contains(name, "vmware") || strings.Contains(name, "virtual") || strings.Contains(name, "vpn") {
				score = -10
			}
			out = append(out, network{ip.String(), it.Name, score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
