# Sub2API 0.2.21-r1 Linux 镜像

## 发布准备（2026-11-11，Asia/Shanghai）

目标镜像 `ghcr.io/ttt599536561/sub2api-custom:0.2.21-r1`，仅 `linux/amd64` 与 `linux/arm64`。本阶段尚未发布；最终结果、控制提交与 digest 待验证后补录。应用源码固定为 `8189812c728ee29c1d57977f553f6a72498f3ca5`，源码版本 0.2.21，构建时注入二开发行号 0.2.21-r1。

应用包括官方 v0.2.21、本地 Codex/CC Switch 修正、两轮 TTL/匿名 WS 结算及配额缓存审查修复，保留 QQ、NapCat、福利、推广及 Ultrafast 二开。初次源码 29bb1ea62 的云端安全扫描发现 Go 标准库漏洞；最终应用已统一 Go 1.27.4，来源与实际验证见[发布前修正记录](../docs/changes/2026-11-11-v0221-release-preflight.md)。未增加安全豁免或降低发布门禁。

专用工作流 `publish-custom-v0221-r1.yml` 仅手动触发，固定源码的 main/push CI 与 Security 均成功后，使用两套原生 Ubuntu runner 构建并实际运行镜像。检查发行版本、精确提交、OCI 标签、Go 版本、运行资源及 NapCat/TTL/匿名结算/前端组件标记，然后发布不可覆盖的最终 tag；保留五份验证产物。macOS 工作流没有触发，不推送 v* Git 标签。

## 升级注意

相对 0.2.20-r3 新增官方迁移 `249_add_usage_logs_api_key_cost_index_notx.sql` 与 `250_openai_cache_write_ttl.sql`，迁移总数 312→314；完整文件名是迁移标识，不改旧 SQL/checksum。用量索引用 CONCURRENTLY 建立，大表首次升级可能耗时。迁移新装及旧账户/福利/QQ/NapCat/MCP 数据保全验证见[合并记录](../docs/changes/2026-11-11-upstream-v0221.md)。

升级保留当前 Compose 项目、数据库/Redis/NapCat 卷、环境、QQ密钥及会话，先备份再替换应用镜像。程序修复只作用于新请求，未追补历史计费。镜像发布不等于服务器部署，本轮不操作生产或测试服务器。

## 验证状态

- 精确应用 CI/Security：等待云端最终结果。
- 双架构构建与运行：尚未触发。
- 五份 ZIP 的大小、SHA256、CRC、文件集合及 run/control/source/arch 绑定：待执行。
- GHCR 匿名 index/config/全部运行层完整性与最终 tag 稳定性：待执行。

本轮本机证据位于 `.cache/release-v0221-r1-20261111/`，验证脚本已适配新版本、Go 1.27.4 并通过 21 项离线测试；复用既有依赖和镜像层缓存，不保存凭据。
