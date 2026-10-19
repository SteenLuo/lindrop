package main

// Arrival state is separate from transfer progress: one task alerts once, and
// a file's progress/completion cannot repeatedly count as a new message.
type incomingNotices struct {
	initialized bool
	seen        map[string]bool
	unread      map[string]string
}

func (v *incomingNotices) update(tasks []Task, self, viewedPeer string) []Task {
	if v.seen == nil {
		v.seen = map[string]bool{}
		v.unread = map[string]string{}
	}
	var arrived []Task
	for _, t := range tasks {
		peer := conversationPeer(t, self)
		if t.Direction != "in" || peer == "" {
			continue
		}
		if !v.initialized {
			v.seen[t.ID] = true
			continue
		}
		ready := t.Status == "waiting" || t.Status == "done"
		if t.Kind == "text" {
			ready = t.Status == "done"
		}
		if ready && !v.seen[t.ID] {
			v.seen[t.ID] = true
			v.unread[t.ID] = peer
			arrived = append(arrived, t)
		}
	}
	v.initialized = true
	if viewedPeer != "" {
		for id, peer := range v.unread {
			if peer == viewedPeer {
				delete(v.unread, id)
			}
		}
	}
	return arrived
}
func (v *incomingNotices) count(peer string) int {
	n := 0
	for _, p := range v.unread {
		if peer == "" || p == peer {
			n++
		}
	}
	return n
}
