package main

// One pending height, not a queue of pointer positions. The UI timer owns
// presentation cadence; mouse input only replaces the target for the next frame.
type composerDragFrame struct {
	Height         int
	Pending, Armed bool
}

func (f *composerDragFrame) request(height int) bool {
	f.Height, f.Pending = height, true
	if f.Armed {
		return false
	}
	f.Armed = true
	return true
}

func (f *composerDragFrame) take() (int, bool) {
	if !f.Pending {
		return 0, false
	}
	f.Pending = false
	return f.Height, true
}

func (f *composerDragFrame) reset() { *f = composerDragFrame{} }
