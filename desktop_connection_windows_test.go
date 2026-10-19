package main

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestPhonePopoverPositionAndDismissal(t *testing.T) {
	for _, c := range []struct {
		anchor, work rect
	}{
		{rect{1000, 50, 1100, 87}, rect{0, 0, 1200, 700}},
		{rect{20, 600, 120, 637}, rect{0, 0, 1200, 700}},
		{rect{-1800, 100, -1700, 137}, rect{-1920, 0, 0, 1080}},
		{rect{50, 20, 150, 57}, rect{0, 0, 250, 400}},
	} {
		p := phonePopoverRect(c.anchor, c.work, 320, 438, 8)
		if p.L < c.work.L || p.T < c.work.T || p.R > c.work.R || p.B > c.work.B {
			t.Fatalf("popover outside work area: %+v in %+v", p, c.work)
		}
	}
	a, p := rect{1000, 50, 1100, 87}, rect{780, 95, 1100, 533}
	for _, c := range []struct {
		p              point
		keyboard, want bool
	}{
		{point{1050, 65}, false, true},
		{point{1050, 91}, false, true}, // Moving through the anchor/card gap.
		{point{900, 300}, false, true},
		{point{500, 300}, false, false},
		{point{500, 300}, true, true}, // Keyboard focus retains the card.
	} {
		if got := phonePopoverContains(c.p, a, p, 8, c.keyboard); got != c.want {
			t.Fatalf("point %+v, keyboard %v: got %v", c.p, c.keyboard, got)
		}
	}
}

func TestDesktopPairInputAndErrors(t *testing.T) {
	for _, c := range []struct {
		addr, code string
		field      int
	}{
		{"", "123456", 113},
		{"192.168.50.20:0", "123456", 113},
		{"192.168.50.20:65536", "123456", 113},
		{"8.8.8.8:8776", "123456", 113},
		{"192.168.50.20:8776", "", 0},
		{"192.168.50.20:8776", "１２３４５６", 0},
		{"192.168.50.20:8776", "12345a", 0},
		{"192.168.50.20:8776", " 012345 ", 0},
		{" http://192.168.50.20:8776/ ", "012345", 0},
		{"192.168.50.20", "012345", 0},
	} {
		addr, code, field, message := desktopPairInput(c.addr, c.code)
		if field != c.field || (field != 0 && message == "") {
			t.Fatalf("%q/%q: field %d, message %q", c.addr, c.code, field, message)
		}
		if field == 0 && (addr != "192.168.50.20:8776" || code != strings.TrimSpace(c.code)) {
			t.Fatalf("normalization lost port/leading zero: %q/%q", addr, code)
		}
	}
	for _, e := range []error{io.EOF, fmt.Errorf("handshake: %w", io.ErrUnexpectedEOF)} {
		got := desktopPairError(e).Error()
		if !strings.Contains(got, "关闭了连接") || strings.Contains(got, "EOF") || strings.Contains(got, "配对码错误") {
			t.Fatalf("misleading connection error: %s", got)
		}
	}
}
