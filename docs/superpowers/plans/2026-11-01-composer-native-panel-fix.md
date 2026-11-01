# 原生输入面板绘制修正

替代0.7.8针对分隔线重绘的实现；保留输入高度调整及截图发送交互。

## 已核对资料

- Microsoft SetWindowPos：SWP_NOCOPYBITS丢弃客户区内容；SWP_NOREDRAW使应用负责全部受影响区域的重画。
  https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwindowpos
- Microsoft RedrawWindow：RDW_NOERASE压制待处理的背景擦除；RDW_ERASE使标准控件在重画时清理背景。
  https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-redrawwindow
- Microsoft Extended Window Styles：WS_EX_COMPOSITED对后代窗口使用系统双缓冲；不能与CS_OWNDC/CS_CLASSDC/CS_PARENTDC组合。
  https://learn.microsoft.com/en-us/windows/win32/winmsg/extended-window-styles
- WTL CSplitterImpl使用普通SetWindowPos更新面板，完整拖动时UpdateWindow。仅参考公开实现方式，不引入WTL依赖。
  https://github.com/Win32-WTL/WTL/blob/main/Include/atlsplit.h
- Raymond Chen说明合成缓冲在ReleaseDC/EndPaint后呈现；持有窗口DC会造成约100ms的更新延迟。
  https://devblogs.microsoft.com/oldnewthing/20171018-00/?p=97245

## 实现

1. 输入区作为独立子窗口，开启WS_EX_COMPOSITED与WS_EX_CONTROLPARENT，只有原生EDIT及三个工具/发送按钮位于此子树。
2. 上层分隔布局更新对话与输入面板；面板内布局独立批量调整。每批窗口有共同父窗口。使用普通有效像素保留和背景清理，不复用0.7.8的NOREDRAW/NOCOPYBITS/NOERASE组合。
3. 自绘面板在WM_PAINT完整填底并绘制装饰；允许原生EDIT处理背景擦除。完成完整子树绘制后呈现。主对话与视频不启用整窗合成缓冲。
4. 按父窗口坐标布局并转换输入子控件局部坐标；转发命令、owner-draw、颜色与拖放消息；鼠标坐标映射回主窗口；保留Tab递归导航。
5. 升级验证：隔离进程运行生产窗口，跨消息循环捕捉真正呈现的画面，读取空白输入区像素并保存仅含生成示例的证据。对照旧实现不主动补画标准控件。
