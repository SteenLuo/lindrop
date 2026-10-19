package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
)

func main() {
	dir := flag.String("state-dir", "", "设备配置与任务目录")
	ip := flag.String("ip", "", "局域网 IPv4")
	port := flag.Int("port", 8776, "节点端口")
	name := flag.String("name", "", "设备名称")
	save := flag.String("save", "", "接收目录")
	headless := flag.Bool("headless", false, "后台测试模式")
	flag.Parse()
	if *dir == "" {
		base, e := os.UserConfigDir()
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			return
		}
		*dir = filepath.Join(base, "LinDropNode")
	}
	instance, fresh := acquireInstance(*dir)
	if !fresh {
		activateExisting(*dir)
		return
	}
	defer call(kernel.NewProc("CloseHandle"), instance)
	n, e := NewNode(*dir, *name, *save)
	if e != nil {
		showError(e.Error())
		return
	}
	if e = n.Start(*ip, *port); e != nil {
		showError(e.Error())
		return
	}
	defer n.Close()
	raw, _ := json.Marshal(map[string]string{"id": n.id, "name": n.name(), "addr": n.addr, "local": n.localAddr, "admin": n.admin, "code": n.code, "join": "http://" + n.addr + "/#join=" + n.webToken})
	fmt.Println(string(raw))
	if *headless {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		select {
		case <-signals:
		case <-n.stop:
		}
		return
	}
	runDesktop(n)
}
