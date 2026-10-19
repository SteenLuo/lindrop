package main

import "testing"

func TestDesktopActionsRespectTransferState(t *testing.T) {
	cases := []struct {
		task   Task
		action int
	}{
		{Task{Direction: "in", Status: "waiting"}, 127},
		{Task{Direction: "out", Status: "sending"}, 130},
		{Task{Direction: "in", Status: "paused"}, 131},
		{Task{Direction: "in", Status: "done"}, 153},
		{Task{Direction: "out", Status: "done"}, 0},
		{Task{Direction: "webout", Status: "waiting_browser"}, 0},
		{Task{Direction: "web", Status: "receiving"}, 0},
		{Task{Direction: "in", Status: "rejected"}, 0},
		{Task{Direction: "out", Status: "preparing"}, 0},
	}
	for _, c := range cases {
		id, _ := taskAction(c.task)
		if id != c.action {
			t.Errorf("%s/%s: action %d, want %d", c.task.Direction, c.task.Status, id, c.action)
		}
	}
}
