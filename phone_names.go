package main

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// A name suggestion is deliberately not an authentication/device credential.
// Choosing one changes only the current browser's label, never its mailbox ID.
type PhoneName struct {
	Ref   string `json:"ref"`
	Name  string `json:"name"`
	Entry string `json:"entry"`
}

func (n *Node) localPhoneNames() []PhoneName {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []PhoneName
	for _, c := range n.cfg.Clients {
		if c.Name == "" || c.Name == "我的手机" {
			continue
		}
		out = append(out, PhoneName{"web:" + n.id + ":" + c.ID, c.Name, n.cfg.Name})
	}
	sortPhoneNames(out)
	return out
}
func sortPhoneNames(out []PhoneName) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Ref < out[j].Ref
	})
}
func (n *Node) availablePhoneNames() []PhoneName {
	n.phoneNamesMu.Lock()
	defer n.phoneNamesMu.Unlock()
	out := n.localPhoneNames()
	if time.Now().Before(n.phoneNamesUntil) {
		out = append(out, n.phoneNamesCache...)
		sortPhoneNames(out)
		return out
	}
	var targets []Device
	for _, d := range n.devices() {
		if d.ID != n.id && d.Online && !strings.HasPrefix(d.ID, "web:") {
			targets = append(targets, d)
		}
	}
	if len(targets) > 16 {
		targets = targets[:16]
	}
	results := make(chan []PhoneName, len(targets))
	for _, d := range targets {
		go func(d Device) {
			var names []PhoneName
			if n.Call(d.ID, "phone.names", struct{}{}, &names) != nil {
				names = nil
			}
			valid := []PhoneName{}
			for _, name := range names {
				if strings.HasPrefix(name.Ref, "web:"+d.ID+":") && name.Name != "" && len([]rune(name.Name)) <= 30 {
					name.Entry = d.Name
					valid = append(valid, name)
				}
				if len(valid) >= 100 {
					break
				}
			}
			results <- valid
		}(d)
	}
	var remote []PhoneName
	timer := time.NewTimer(1800 * time.Millisecond)
	defer timer.Stop()
	for range targets {
		select {
		case names := <-results:
			remote = append(remote, names...)
		case <-timer.C:
			goto collected
		case <-n.stop:
			goto collected
		}
	}
collected:
	unique := map[string]PhoneName{}
	for _, name := range remote {
		unique[name.Ref] = name
	}
	remote = nil
	for _, name := range unique {
		remote = append(remote, name)
	}
	sortPhoneNames(remote)
	n.phoneNamesCache = remote
	n.phoneNamesUntil = time.Now().Add(5 * time.Second)
	out = append(out, remote...)
	sortPhoneNames(out)
	return out
}
func (n *Node) usePhoneName(c WebClient, ref string) (string, error) {
	name := ""
	for _, p := range n.availablePhoneNames() {
		if p.Ref == ref {
			name = p.Name
			break
		}
	}
	if name == "" {
		return "", errors.New("这个手机名称暂不可用，请刷新或手动输入")
	}
	n.mu.Lock()
	current, ok := n.cfg.Clients[c.Key]
	if !ok || current.ID != c.ID {
		n.mu.Unlock()
		return "", errors.New("请重新扫码后选择手机名称")
	}
	current.Name = name
	n.cfg.Clients[c.Key] = current
	n.mu.Unlock()
	if e := n.persist(); e != nil {
		return "", e
	}
	n.announce()
	return name, nil
}
