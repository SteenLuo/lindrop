package main

type NoticeSettings struct {
	Sound   bool `json:"sound"`
	Badges  bool `json:"badges"`
	Title   bool `json:"title"`
	Taskbar bool `json:"taskbar"`
	Popup   bool `json:"popup"`
	InApp   bool `json:"inApp"`
}

func defaultNoticeSettings() NoticeSettings {
	return NoticeSettings{true, true, true, true, true, true}
}
func (n *Node) noticeSettings() NoticeSettings {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cfg.Notifications == nil {
		return defaultNoticeSettings()
	}
	return *n.cfg.Notifications
}
func (n *Node) SetNoticeSettings(s NoticeSettings) error {
	n.mu.Lock()
	copy := s
	n.cfg.Notifications = &copy
	n.mu.Unlock()
	return n.persist()
}
func (s NoticeSettings) values() []bool {
	return []bool{s.Sound, s.Badges, s.Title, s.Taskbar, s.Popup, s.InApp}
}
func (s *NoticeSettings) toggle(i int) {
	switch i {
	case 0:
		s.Sound = !s.Sound
	case 1:
		s.Badges = !s.Badges
	case 2:
		s.Title = !s.Title
	case 3:
		s.Taskbar = !s.Taskbar
	case 4:
		s.Popup = !s.Popup
	case 5:
		s.InApp = !s.InApp
	}
}
