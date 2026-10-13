---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-13
---

# Reasonix 安全审计证据

对应 [审计报告](../security-audit-2026-10-12.md)，源码版本 `d1f096b206af471f558029fc11316c21ee6aae57`。

`probes/` 保存五项本地探针源码。它们断言当前危险行为可以发生，**通过代表缺陷已复现**；修复后的代码应使对应探针失败，然后将其改成断言拒绝危险行为的 regression test。使用 `.go.txt` 后缀，避免把依赖当前包测试 helper 的证据混入正常 Go package。

复现仅用虚构数据和 loopback 服务，不登录账号，不启动真实模型，不连接真实中继，不读取生产凭据，不安装更新。`run-probes.py` 通过 Go overlay 临时替换既有测试文件，应用与正常测试文件的磁盘内容保持不变。替换只为了编译探针，不是跳过正常测试来声称产品安全；原有测试结果另有日志。

在仓库根执行：

```bash
python3 docs/audits/security-audit-2026-10-12/run-probes.py
```

脚本使用仓库锁定的 Go 1.26.9，首次执行可能下载工具链或依赖。它核实五个探针都实际运行，避免将 `no tests to run` 误计为通过。

| 文件 | 内容 |
| --- | --- |
| `probes.log`、`config-probe.log` | 初次成功复现的详细日志，运行工具链为本机 Go 1.26.5 |
| `probes-go1269.log` | 可重跑脚本在锁定 Go 1.26.9 下再次确认五个探针 |
| `security-tests.log` | 八个安全相关包的原始全套结果，包含安装源在 umask=077 下的权限断言失败 |
| `installsource-umask022.log` | Go 1.26.9、独立 shell 的 umask=022 下安装源全套通过 |
| `serve-tests.log` | CloudDesktop、认证、附件等选定测试通过 |
| `project-scope-tests.log`、`sandbox-tests.log` | Go 1.26.9 下项目权限与 Linux sandbox 选定测试，保留平台 skip |
| `govulncheck-go1269-modules.log` | module 告警；不能视为已调用存在问题的代码 |
| `govulncheck-go1269-imports.log` | CLI/桌面入口导入包级扫描 |
| `npm-audit-*.json` | frontend/Workers production dependencies，以及 Electron 构建依赖的公共数据库结果 |
| `network-literals.json` | 源码和构建配置中的字面 URL，不是抓包结果，不证明所有地址都会连接 |

Electron 原有单元测试另执行 `node --test desktop/electron/test/unit.mjs`，结果为 109 pass、3 macOS-only skip、0 fail。本目录没有保存该次全量 stdout。

告警查询时间为 2026 年 10 月 12 日。以后数据库、依赖版本及实现变化可能改变结果；更新报告应重新扫描并重新核实可达性。

## 修复后的验证

[修复与依赖补充记录](../security-remediation-2026-10-12.md) 描述当前行为；原始探针仍用于复现旧版危险行为，不能用于声称修复通过。

`dependencies.json` 区分 Go 模块图和默认 Linux 链接子集；`external-hosts.json` 只是字面 URL 的 host 索引。`fix-validation.json` 保存根 Go module 最终全套计数及失败用例，`fix-full-tests-final.jsonl.gz` 保存原始输出。

`fix-network.log`、`fix-electron.log`、`fix-check.log`、`fix-lint.log`、`fix-coverage.log`、`fix-vet.log`、`fix-frontend.log` 保存相应验证。`fix-existing-memory.log`、`fix-existing-bootstrap.log` 保存原版源码 overlay 的既有失败。

`fix-baseline-*.jsonl` 是 F1–F5 新回归在旧版行为上的失败证据；`fix-tests.jsonl.gz` 与 `fix-full-tests.jsonl.gz` 是中间运行记录，其中包含后来修正的测试/transport 问题，以最终验证为准。

## 当前 clean fork 验证（2026-10-13）

`clean-fork-validation.json` 和 `clean-*.log` 对应关闭内置后台服务、恢复正常 agent 功能的当前实现。`model-only-*` 是已撤回的严格隔离方案历史证据，不是当前保证或启动要求。外部 host 索引保留原始引用位置，更新了当前策略分类。
