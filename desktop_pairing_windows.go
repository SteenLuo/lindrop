package main

import (
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
)

var guiPairError string

func desktopPairInput(addr, code string) (string, string, int, string) {
	addr, code = strings.TrimSpace(addr), strings.TrimSpace(code)
	addr = strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
	addr = strings.TrimRight(addr, "/")
	if !strings.Contains(addr, ":") && addr != "" {
		addr += ":8776"
	}
	host, port, e := net.SplitHostPort(addr)
	p, portErr := strconv.Atoi(port)
	ip := net.ParseIP(host)
	if e != nil || ip == nil || ip.To4() == nil || !privateAddr(addr) || portErr != nil || p < 1 || p > 65535 {
		return addr, code, 113, "请填写对方地址，例如 192.168.1.10:8776"
	}

	return addr, code, 0, ""
}

func desktopPairError(e error) error {
	if errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF) {
		return errors.New("对方在连接完成前关闭了连接。请确认对方运行的是邻传客户端，地址和端口正确；也可在对方电脑上反向连接本机。")
	}
	var ne net.Error
	if errors.As(e, &ne) && ne.Timeout() {
		return errors.New("连接超时。请确认对方正在运行，地址、端口和网络可达；也可从对方电脑反向连接本机。")
	}
	return e
}
