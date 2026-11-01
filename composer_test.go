package main

import "testing"

func TestComposerGeometryPreservesConversationAndFooter(t *testing.T) {
	for _, height := range []int{570, 600, 654, 770, 1200} {
		for _, requested := range []int{-100, 0, 64, 96, 260, 5000} {
			for _, pictures := range []int{0, 1, 8} {
				g := composerLayout(height, requested, pictures)
				if g.ListHeight < 160 || g.TextHeight < 64 || g.Top != 74+g.ListHeight {
					t.Fatal("input resizing consumed conversation", height, requested, pictures, g)
				}
				if g.TextTop+g.TextHeight != height-45 || g.TextTop < g.Top+44 {
					t.Fatal("text overlaps toolbar or send/footer", g)
				}
				if pictures > 0 && g.PreviewTop+70 > g.TextTop {
					t.Fatal("thumbnails overlap input", g)
				}
			}
		}
	}
	requested := 300
	_ = composerLayout(570, requested, 8)
	if g := composerLayout(1000, requested, 0); g.TextHeight != requested {
		t.Fatal("small-window clamp overwrote requested size")
	}
}
