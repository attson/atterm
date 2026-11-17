---
description: 从 Web、PWA、iPhone、iPad 或另一台桌面客户端登录和配对 AT Term，并正确使用 viewer、driver 与远程权限。
---

# 跨设备客户端

桌面 host 负责运行 PTY；Web、PWA、iOS 或另一台桌面客户端负责 attach。远程客户端离开后任务仍在 host 上运行，重新打开同一 session 即可继续。

## 选择客户端

| 客户端 | 适合场景 | 主要限制 |
|---|---|---|
| 另一台桌面端 | 长时间完整操作、键鼠输入 | 需要安装应用并登录同一 Relay 或加入同一 Peer Space |
| 浏览器 | 临时查看和接管 | 生产环境必须使用 HTTPS；浏览器关闭后本地 tab 状态可能丢失 |
| PWA | 像应用一样固定入口、接收 Web Push | iOS 通知要求先“添加到主屏幕”，普通 Safari tab 不支持 |
| iPhone / iPad | 移动查看、短输入、扫码配对 | 当前需从源码构建；不在设备上启动本地 PTY 或 SSH |

## 登录或配对

使用 Relay 时，在浏览器登录页或客户端设置中输入同一账号。iOS 也可以扫描桌面端生成的 5 分钟一次性配对码；成功后凭据进入 Keychain，不写入浏览器 localStorage。

使用 Peer Space 时，在已有成员上创建邀请或连接 bundle，再在新设备导入。Relay 与 Peer 的选择见 [远程接管与会话侧栏](/guide/remote-takeover)。

## Viewer、driver 与权限

新 attach 的远程客户端默认是 **viewer**，可以观看输出但不能输入。按界面提示取得控制权后成为 **driver**；同一时间的控制权由 host/relay 协调，重新可见时不会静默抢走别人的 driver。

桌面 owner 还会为每个会话设置远程权限：

| 权限 | 查看输出 | 输入 / resize | 图片、文件与文件系统 |
|---|---:|---:|---:|
| `view` | 是 | 否 | 否 |
| `control` | 是 | 是 | 否 |
| `full` | 是 | 是 | 是 |

成为 driver 不会绕过权限：`view` 会话即使尝试接管也保持只读。权限由 relay 和桌面 host 双重检查。

## 移动输入

Web 与 iOS 终端提供 Enter、Esc、Tab、`Ctrl-C`、`Ctrl-D` 和方向键辅助条。iOS 中文输入法的组合输入由终端专门处理；viewer 模式锁定到 host 的 PTY 尺寸，避免手机屏幕 fit 改变正在运行程序的布局。

网络恢复后客户端会按输出序号续接；大段历史回放期间会显示进度。若能看到输出但无法输入，依次检查 driver 状态、会话权限和连接健康，再看 [FAQ / 故障排查](/guide/faq)。
