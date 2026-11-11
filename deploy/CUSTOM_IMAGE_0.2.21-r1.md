# Sub2API 0.2.21-r1 Linux 镜像

## 已发布并完成验证（2026-11-11，Asia/Shanghai）

镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.21-r1`。

固定内容：`ghcr.io/ttt599536561/sub2api-custom@sha256:7749490a780211cc4fc08a958dee590baed93f15bf72efad4a8b49c1b8abc32a`。

应用源码 `8189812c728ee29c1d57977f553f6a72498f3ca5`；发布控制 `0772c56ba916ef12e961c76f7bac252b8f3cb10a`。仅 `linux/amd64` 与 `linux/arm64`，Go 1.27.4，构建时注入版本 0.2.21-r1。后续文档提交不改变镜像来源。

- 应用 [CI 42977703072](https://github.com/Ttt599536561/sub2api/actions/runs/42977703072)与 [Security 42977703055](https://github.com/Ttt599536561/sub2api/actions/runs/42977703055)成功；控制 [CI 42978072811](https://github.com/Ttt599536561/sub2api/actions/runs/42978072811)与 [Security 42978072810](https://github.com/Ttt599536561/sub2api/actions/runs/42978072810)成功。
- [发布 42979739284](https://github.com/Ttt599536561/sub2api/actions/runs/42979739284)成功。两套原生 Linux runner 实际构建并运行对应镜像，版本/精确提交/OCI 标签/Go 构建信息、PostgreSQL 工具、定价资源、setup 状态、前端及 NapCat/TTL/匿名轮结算标记均通过。
- 北京时间15:34:49，五份 ZIP 大小、SHA256、CRC、精确文件集合以及 run/control/source/arch 绑定通过。
- 北京时间15:35:25，匿名 index/运行 manifest/config/Actions 绑定及全部24个唯一运行层通过，总计92,498,744字节；14层复用并重新哈希、匿名HEAD，10层新下载78,245,333字节。最终tag稳定，digest与发布日志独立读数一致。
- 未构建 macOS，未触发 Apple工作流或通用 v* tag发布；未操作生产/测试服务器。

应用包括官方 v0.2.21、本地 Codex/CC Switch 修正、两轮 TTL/匿名 WS 结算及配额缓存审查修复，保留 QQ、NapCat、福利、推广及 Ultrafast 二开。初次源码29bb1ea62的完整CI成功，但安全检查发现Go1.27.3标准库GO-2026-6859，故最终升级1.27.4并重新通过全部门禁。失败与修正见[发布前记录](../docs/changes/2026-11-11-v0221-release-preflight.md)，没有增加安全豁免或降低门禁。

## 使用与升级

```bash
docker pull ghcr.io/ttt599536561/sub2api-custom:0.2.21-r1
```

相对0.2.20-r3新增官方迁移 `249_add_usage_logs_api_key_cost_index_notx.sql` 与 `250_openai_cache_write_ttl.sql`，迁移总数312→314；完整文件名是迁移标识，不改旧SQL/checksum。用量索引用CONCURRENTLY建立，大表首次升级可能耗时。新装及旧账户/福利/QQ/NapCat/MCP数据保全验证见[合并记录](../docs/changes/2026-11-11-upstream-v0221.md)。

升级保留当前Compose项目、数据库/Redis/NapCat卷、环境、QQ密钥及会话，先备份再替换应用镜像。程序修复只作用于新请求，未追补历史计费。镜像发布不等于服务器部署，本轮未在线复核或更新生产/测试实例。

## 证据与本机保全

本机证据 `.cache/release-v0221-r1-20261111/publish/verification-0.2.21-r1/` 内含产物、registry与最终摘要；验证器适配新版本/Go1.27.4并通过21项离线测试。复用既有依赖与镜像层缓存，无残留.partial；一次性本地验证二进制已清理。原有dist、调查文件和本机环境脚本保留且未纳入镜像，不保存凭据。
