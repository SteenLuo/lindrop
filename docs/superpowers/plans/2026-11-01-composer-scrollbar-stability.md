# 输入滚动条缩放稳定性修复

用户报告：输入区右侧底部箭头只在调整大小时跳动，静置正常。

## 证据与边界

0.7.11原生几何检查显示滚动条底端没有移动。扩展远程画面检查后，在2倍应用布局下复现：x64的240次输入/窗口高度变化中有5帧箭头像素变化，x86有10帧；保存的输入高度变化画面中箭头短暂消失，随后恢复。该比例由隔离进程布局与字体模拟，不等同于更改系统DPI。只包含生成的示例，不上传用户截图、配置或真实消息。

## 修复

输入文字继续使用标准EDIT，移除其WS_VSCROLL。使用同一WS_EX_COMPOSITED面板内的原生SCROLLBAR子控件，避免内置非客户区滚动条的独立呈现路径；输入和滚动条共享同父窗口批量布局。右侧总宽度与位置保留，滚动条宽度读取系统指标。

原生EDIT维护文字、折行、选择与输入法；按原生折行数、格式矩形、字体行高及首个可见行同步SCROLLINFO。同步值相同时不重绘；字体测量使用内存DC，不在调整中释放输入窗口DC引发缓冲提前呈现。滑块通过32位TrackPos与EM_LINESCROLL移动，其他导航沿用原生编辑命令；滚轮遵守系统行数/整页设置并累计小于一个刻度的增量，文字区和滚动条上均可滚动。不增加Tab停靠点。

## 验证与交付

1. 隐藏窗口检查实际原生折行/滚动范围、翻页、箭头、滚轮、选区不变、调整高度、空文复位及超过65535行的位置，检查底端几何固定。
2. 一次远程子进程检查240次输入/窗口高度变化，连续捕捉底部箭头区域，与空白输入基准逐像素比较；分别检查默认和2倍应用布局，保留旧版失败证据。正常空白输入的底部箭头不应随高度变化改变像素。
3. 保留窗口最大化/还原、工具栏、固定底栏和连续拖动性能检查，以及完整双架构回归。实际Win7/Win10、用户显示器DPI及显卡环境仍需使用者复核，不宣称远程模拟覆盖所有实体环境。
4. 发布0.7.12程序包和源码，沿用既有配置位置、身份和传输协议。

参考：Microsoft RedrawWindow、WM_NCPAINT、SCROLLINFO与EM_LINESCROLL文档。非客户区绘制与客户区绘制分别处理，不能用稳定的窗口坐标代替实际画面稳定性检查。

- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-redrawwindow
- https://learn.microsoft.com/en-us/windows/win32/gdi/wm-ncpaint
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-scrollinfo
- https://learn.microsoft.com/en-us/windows/win32/controls/em-linescroll
