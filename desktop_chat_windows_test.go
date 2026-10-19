package main

import (
	"strings"
	"testing"
)

func TestConversationOwnershipAndOrder(t *testing.T) {
	all := []Task{
		{ID: "later", Direction: "out", Sender: "me", Target: "peer", Created: 10, CreatedNS: 10000000002},
		{ID: "first", Direction: "in", Sender: "peer", Target: "me", Created: 10, CreatedNS: 10000000001},
		{ID: "other", Direction: "in", Sender: "other", Target: "me", Created: 1},
		{ID: "relay", Direction: "webout", Sender: "web:me:phone", Target: "peer", Created: 1},
		{ID: "phone-inbox", Direction: "web", Sender: "peer", Target: "web:me:phone", Created: 1},
		{ID: "download", Direction: "in", Sender: "me", Target: "peer", Pull: true, Created: 11},
	}
	got := conversationTasks(all, "me", "peer")
	if len(got) != 3 || got[0].ID != "first" || got[1].ID != "later" || got[2].ID != "download" {
		t.Fatalf("wrong conversation: %+v", got)
	}
	if len(conversationTasks(all, "me", "")) != 0 {
		t.Fatal("no device must show no conversation")
	}
}

func TestChatWrappingAndActionGeometry(t *testing.T) {
	oldScale, oldWidth := guiScale, guiWidth
	guiScale = 1
	guiWidth = 1200
	defer func() { guiScale = oldScale; guiWidth = oldWidth }()
	chatTextCache["wrap"] = "你好\r\n第二行 😀 " + strings.Repeat("长文字", 200)
	defer delete(chatTextCache, "wrap")
	incoming := Task{ID: "wrap", Kind: "text", Direction: "in", Status: "done"}
	outgoing := incoming
	outgoing.Direction = "out"
	a, b := chatBubbleLayout(incoming, 700), chatBubbleLayout(outgoing, 700)
	if len(a.Lines) > 5 || !a.More || a.Lines[0] != "你好" || b.X <= a.X || b.X+b.W > 700 {
		t.Fatalf("bad wrapped bubbles: %+v / %+v", a, b)
	}
	r := chatActionRect(incoming, a)
	if id := chatHit(incoming, 700, int(r.L)+2, int(r.T)+2); id != 0 {
		t.Fatal("copy exposed without hover")
	}
	chatHoverID = incoming.ID
	defer func() { chatHoverID = "" }()
	if id := chatHit(incoming, 700, int(r.L)+2, int(r.T)+2); id != 172 {
		t.Fatalf("copy hit %d", id)
	}
	waiting := Task{Direction: "in", Status: "waiting"}
	c := chatBubbleLayout(waiting, 700)
	r = chatSecondaryRect(waiting, c)
	if id := chatHit(waiting, 700, int(r.L)+2, int(r.T)+2); id != 129 {
		t.Fatal("reject hit missing")
	}
}

func TestExpandedChatPreservesFullTextInBoundedRows(t *testing.T) {
	body := strings.Repeat("展开的中文与emoji😀，每一行都保留。\n", 600) + "最后一行"
	task := Task{ID: "full-expansion", Kind: "text", Direction: "in", Status: "done"}
	chatTextCache[task.ID] = body
	defer func() { delete(chatTextCache, task.ID); delete(chatExpanded, task.ID) }()
	collapsed := chatMessageParts(task, 650)
	if len(collapsed) != 1 || !collapsed[0].More {
		t.Fatal("long message was not folded")
	}
	oldScale := guiScale
	defer func() { guiScale = oldScale }()
	for _, scale := range []float64{1, 1.25, 1.5, 2} {
		guiScale = scale
		chatExpanded[task.ID] = true
		parts := chatMessageParts(task, 650)
		if len(parts) < 2 || !parts[0].First || !parts[len(parts)-1].Last {
			t.Fatal("full message not split into continuous rows")
		}
		var joined []string
		for _, p := range parts {
			b := chatBubblePart(task, 650, &p)
			height := b.Y + b.H
			if b.Last {
				height += 27
			}
			if sc(height) > 255 || height <= 0 {
				t.Fatalf("native list row exceeds limit at scale %g: %dpx", scale, sc(height))
			}
			joined = append(joined, p.Lines...)
		}
		if strings.Join(joined, "") != strings.ReplaceAll(body, "\n", "") {
			t.Fatal("expanded message lost text")
		}
		if chatBody(task) != body {
			t.Fatal("copy no longer returns original text")
		}
		chatExpanded[task.ID] = false
		for _, p := range chatMessageParts(task, 650) {
			b := chatBubblePart(task, 650, &p)
			height := b.Y + b.H
			if b.Last {
				height += 27
			}
			if sc(height) > 255 {
				t.Fatal("folded row exceeds physical height limit")
			}
			if p.Expanded || !p.More {
				t.Fatal("message did not fold back")
			}
		}
	}

}

func TestShortChatBubbleHasNoFullTextAction(t *testing.T) {
	task := Task{ID: "short-density", Kind: "text", Direction: "out", Status: "done"}
	chatTextCache[task.ID] = "好"
	defer delete(chatTextCache, task.ID)
	b := chatBubbleLayout(task, 650)
	if b.More || b.W >= 100 || b.H >= 60 {
		t.Fatalf("short bubble wastes space: %+v", b)
	}
	r := chatSecondaryRect(task, b)
	if id := chatHit(task, 650, int(r.L)+2, int(r.T)+2); id != 0 {
		t.Fatal("short message offers unnecessary expand action")
	}
}

func TestPendingShortTextStatusHasIndependentWidth(t *testing.T) {
	oldScale := guiScale
	defer func() { guiScale = oldScale }()
	task := Task{ID: "pending-short-status", Kind: "text", Direction: "out", Status: "waiting_remote"}
	chatTextCache[task.ID] = "123"
	defer delete(chatTextCache, task.ID)
	for _, scale := range []float64{1, 1.25, 1.5, 2} {
		guiScale = scale
		for _, width := range []int{400, 700, 1100} {
			for _, direction := range []string{"out", "in"} {
				task.Direction = direction
				b := chatBubbleLayout(task, width)
				for _, copying := range []bool{false, true} {
					r := chatTextStatusRect(task, b, width, copying)
					if r.R-r.L < int32(sc(100)) || r.L < 0 || r.R > int32(sc(width)) {
						t.Fatalf("status squeezed at scale %g: %+v", scale, r)
					}
					if copying {
						c := chatActionRect(task, b)
						if r.L < c.R && r.R > c.L {
							t.Fatal("copy overlaps pending status")
						}
					}
				}
			}
		}
	}
}
