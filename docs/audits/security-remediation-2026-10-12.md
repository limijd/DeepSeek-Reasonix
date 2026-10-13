---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-13
---

# Reasonix 修复与依赖补充审查

基于 [原始审计](security-audit-2026-10-12.md) 的源码版本 `d1f096b206af471f558029fc11316c21ee6aae57`。原报告描述最初审计时的行为；本文件描述当前 fork。

**当前目标是正常工作的 agent，并关闭自带后台服务。此前的 DeepSeek-only 策略、强制 Linux namespace 和子进程禁用已撤回。F1–F5 漏洞修复保留。**

## 当前后台服务边界（2026-10-13）

CLI 和 Studio host 启动时启用固定 fork 策略。用户旧配置中的 telemetry/check_updates 开关不能重启后台服务，环境变量改成其他服务地址也不能绕过组件关闭。

| 通道 | 当前结果 |
| --- | --- |
| 遥测 ping、CLI/Studio metrics | reporter 不启动；待上传队列清理；不会提示首次遥测同意 |
| 上游账号、云中继、注册和重连 | Studio 不创建后台宿主；云中继 Run 直接返回；账号请求入口拒绝 |
| 自动更新、手动升级、release catalog、更新说明和 GitHub 下载回退 | 更新配置固定关闭；更新组件的网络入口拒绝，不能在线安装上游更新 |
| 反馈和崩溃报告上传 | 反馈服务不组装；网络上传入口拒绝；本地崩溃文件仍可保留 |
| 模型账户余额 | 请求入口关闭；缓存读取返回未配置，状态界面不显示故障钱包 |
| `reasonix.io` 及所有子域 | Go 公共 HTTP/stream/proxy 入口、Electron session 与外部链接入口拒绝 |
| `api.typesafe.ai` | 相同入口拒绝；这是可选辅助决策服务，不是新安装默认启动的请求 |
| 本地 Laya 的 HuggingFace SDK 下载和遥测 | 子进程强制 SDK 离线/禁遥测环境；需要本地已有模型 |
| 环境版本探测 | Go 使用本地工具链；npm 关闭更新通知检查 |
| Studio 页面预连接及外部图片 | 删除上游 preconnect、dns-prefetch 和 cloud bootstrap；CSP 禁止远程图片自动加载 |

模型调用、主动模型发现、网页工具、Bash、Git、MCP/LSP、hooks、sidecar、编辑器和普通启动方式恢复原有权限与沙箱行为。用户指定的代理、其他模型端点、LAN 和 loopback 不再被全局禁用。

MCP 可变版本解析只在明确项目启动授权流程中发生；已有 launcher lock 会注入 offline/no-install 标志。用户配置的第三方程序可能自行联网，当前修改没有把它们变成网络隔离进程。它们的自动启动仍属于用户授予的扩展权限，不能以“没有内置遥测”替代扩展审查。

HTTP/session 规则不能拦截任意恶意依赖的原始 TCP、独立 transport、子进程或宿主 IPC。当前结论是关闭已识别的内置后台通道，**不是整个进程树只能访问模型的安全保证**。OS 代理 PAC、自定义工具或已安装第三方扩展仍有各自的信任边界。

## 五项确认缺陷

| 编号 | 修复结果 | 回归验证 |
| --- | --- | --- |
| F1 | 路径附件导入和 `/drop` 只接受已有本地桌面 grant | 云端、设备和普通 token 拒绝；bytes 上传与本地桌面导入保留 |
| F2 | HTTPS 代理先验证 TLS 再发认证；web_fetch 复用 CONNECT 实现；错误凭据脱敏 | TLS 首包、可信/不可信证书、缓冲数据、错误脱敏 |
| F3 | 安装源在代理下仍解析并拒绝内部地址，再使用固定 IP；保留 Host/SNI | metadata、私网、CGNAT、ULA、DNS 变化和 redirect 回归 |
| F4 | deny-only egress 策略组装时报错，避免默默忽略拒绝配置 | 真实 boot.Build 拒绝不支持的策略；已有允许列表继续工作 |
| F5 | 桌面同意设置不接受项目覆盖；上传前重读；隔离 surface 队列 | scope、运行中撤销、上传边界、队列和重新启用回归 |

原版源码 overlay 在 F1–F5 探针中复现危险行为；`fix-baseline-*.jsonl` 记录新增测试在旧版行为下的失败。关闭遥测之外，F5 的修复仍保留。

## 依赖中容易漏查的风险

[Go 依赖清单](security-audit-2026-10-12/dependencies.json) 记录 114 个已解析第三方模块；默认 Linux CLI/Studio host 链接 53 个。不覆盖所有 Node、Python、系统程序和非默认构建。

| 组件 | 已核实的用途与剩余风险 |
| --- | --- |
| Python `laya` | 可选本地决策 SDK，不在 Go/npm 锁文件中；已禁止官方 SDK 自动下载，仍需审查实际安装版本 |
| MCP launcher、sidecar、hooks、LSP | 第三方执行入口；授权和版本锁不能证明内容安全，正常功能恢复后仍需审查 |
| `atotto/clipboard v0.1.4` | 本地 init 查找 PATH helper，复制粘贴时执行系统程序；未见自主外联，helper 信任在锁文件之外 |
| `sabhiram/go-gitignore` commit `525f6e181f06`、`aymanbagabas/go-udiff v0.4.1` | 默认可达的小型解析/diff 包，抽查未见运行时联网/exec；没有证明全部输入的资源上界 |
| `rsc.io/qr v0.2.0` | 默认链接纯 Go 编码包；模块另含非默认 CGO 实现 |
| `go-toast/v2 v2.0.3`、`go-keyring v0.2.8` | Windows 通知及部分平台凭据迁移，未完成锁定版本跨平台源码审查 |
| Tree-sitter / `go-pointer v0.0.1` | 非默认 native/unsafe 解析桥；未完成相应构建审查 |
| `modernc.org/sqlite v1.60.1` / libc | 本地会话数据库；大量生成代码和输入解析面，未逐行审完 |
| Electron、electron-builder | 浏览器解析与安装脚本执行链；本次没有完成真实 GUI/跨平台验证 |
| rehype-raw、rehype-sanitize、KaTeX | 模型输出解析面；原始审计的依赖告警仍需独立处置 |

没有发现足以认定这些依赖植入后门的证据，也没有完成全部依赖审查。小仓库知名度、checksum 和无已知 CVE 都不能单独构成安全背书。

## 验证与 review

本次 correctness review 核对组件调用者、旧配置、缓存状态、直接启动和正常工具路径；security_review 核对关闭是否先于请求、endpoint override、重定向、代理以及 F1–F5 边界。没有修改 lint baseline 或覆盖率 floor。

[当前验证记录](security-audit-2026-10-12/clean-fork-validation.json) 记录本次检查、测试与环境限制。新测试使用虚构凭据、内存 transport 和 loopback；后台入口在 transport 被调用之前拒绝。

`fix-validation.json` 和 `model-only-validation.json` 是此前验证的历史证据。后者对应的 namespace 启动器、broker 和严格测试已移除，不能当作当前 fork 的网络保证，也不再是启动说明。

此前根 Go 全套测试仍有 bootstrap、MemoryBench 和 provider 探测失败；前两项通过原版 production overlay 复现，provider 探测在当前环境超时。本次没有声称这些无关缺陷已修复。

尚未验证真实模型调用、真实桌面运行、Windows/macOS、全部依赖源码和可复现发行构建。不能据此宣布敏感项目已经安全。
