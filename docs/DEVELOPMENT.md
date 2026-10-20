# 开发与构建

## 环境

- Windows，Go 1.20.14，`GOARCH=386`或`amd64`，`CGO_ENABLED=0`。
- `build.ps1` 在项目目录先测试再构建GUI程序。
- 默认测试含回环网络和原生Windows API检查，应在Windows执行；两个需交互的窗口夹具默认跳过。
- 修改浏览器代码后仍需在目标手机浏览器验证，不能把单机测试当作安卓真机验收。

## 目录

| 文件/目录 | 作用 |
| --- | --- |
| `model.go` | 配置、持久身份、设备视图和任务 |
| `link.go` | 双向TLS连接、路由与RPC |
| `link_binary.go`、`transfer_rate.go` | 直接TLS对端协商二进制文件块、签名摘要验证、有限长度帧和速度窗口；未协商或中转走旧路径 |
| `transfer.go`、`text.go` | 文件分块/校验/续传、文字收发 |
| `server.go`、`web/` | HTTP手机入口、发现和网页会话 |
| `desktop_*_windows.go` | 原生窗口、气泡、媒体、拖放、提醒 |
| `desktop_viewport_windows.go` | 对话像素视口、滚轮动画、可见行绘制及32MiB行缓存；设备与共享列表仍为LISTBOX |
| `desktop_selection_windows.go` | 气泡原位选字、原文字偏移映射、跨行及跨绘制段选择，不创建临时EDIT |
| `desktop_text_windows.go` | 保留抗锯齿文字绘制，24MiB文字位图缓存与释放 |
| `notice_settings.go`、`phone_names.go` | 提醒开关、局域网登记名称查询 |
| `assets/`、`icon.ico` | 界面图标 |
| `vendor/` | 随源码分发的Go依赖 |

## 隔离开发节点

每个节点必须有独立配置、接收目录和端口。只做本机测试时显式使用回环地址，例如：

```powershell
.\邻传.exe -state-dir C:\LinDropTest\A -ip 127.0.0.1 -port 18776 -name 测试电脑A -save C:\LinDropTest\ReceiveA
```

不要用真实用户配置运行测试。`-headless` 用于测试进程，不显示窗口。启动时若标准输出被重定向，程序会打印本次管理凭证、节点信息和手机入口信息；这些日志必须留在本机，不能提交或公开。

## 更新窗口图标与清单

需要重新生成资源时，运行以下开发命令（此步需要联网下载资源编译工具）：

```powershell
go run github.com/akavel/rsrc@v0.10.2 -arch 386 -manifest app.manifest -ico icon.ico -o rsrc_windows_386.syso
go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -manifest app.manifest -ico icon.ico -o rsrc_windows_amd64.syso
```

随后完整构建并运行 `TestDesktopEmbeddedIcons`。资源编译工具仅用于开发，不链接进客户端。当前图标组编号为2，修改生成方式时须同步检查实际图标加载。

## 升级与公开发布

先检查没有活动传输和草稿，再正常退出旧实例并备份完整配置目录。保留每台电脑独立身份。不要上传设备配置、私钥、手机cookie、诊断凭证、个人路径与真实聊天截图。

AppData可能受打包开发工具的进程环境影响：即使环境变量和 `-state-dir` 字符串相同，物理文件也可能不同。最终必须从资源管理器双击交付的EXE，检查原名称、接收目录、历史、设备连接，正常退出重开验证。开发启动日志的管理凭证不适用于后续用户启动的进程。

仓库工作流在Windows分别测试并构建x86与x64 GUI程序，上传构建产物供检查。`build.ps1`默认386，使用`-Architecture amd64`生成64位程序。Release附件应只包含程序、说明、许可证和校验值；工作流不自动公开发布Release。

需要避免占用使用者前台时，完整原生回归放在GitHub Windows runner执行，本地只构建。不要启动可见测试窗口或操作使用者鼠标键盘；交互验收需另行安排。测试数据始终与真实节点隔离。
