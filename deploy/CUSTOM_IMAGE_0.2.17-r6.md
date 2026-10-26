# Sub2API 二开 0.2.17-r6 Linux 发布与升级记录

日期：2026-10-26（Asia/Shanghai）。当前为发布准备记录：源码已合入main并推送，等待精确源码GitHub CI/安全门禁与镜像构建；不能据此宣称镜像已发布或服务器已升级。

## 固定来源

- 上游：`ac1b8f1271be0d674fa6f074403c1410f30a3a81`，本轮按用户指定提交，不跟随更晚main。
- 最终应用源码：`02f706a193a1b5c79cd43c4ec5aed85a12553293`；合并提交787d5a4a3的父提交为本仓cda70a59b与指定上游ac1b8f1；最终来源追加了Docker PostgreSQL迁移测试兼容修正及福利集成回归适配，应用业务代码未改。
- 目标镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.17-r6`。
- 平台仅 `linux/amd64`、`linux/arm64`，原生Ubuntu runner构建和运行检查；不触发通用release或macOS任务。
- [专用工作流](../.github/workflows/publish-custom-v0217-r6.yml)固定应用SHA，要求该SHA最新main/push CI及Security Scan成功，禁止覆盖已有r6标签；控制提交和最终digest在发布完成后补充。
- 主目录既有Codex/CCSwitch等11份WIP及非构建untracked文件已保全，不属于本镜像来源。

## 变化与实际验证

合并上游调度快照/配额/流式断连计费/Gemini分组生图开关等改动，保留福利、推广、监控、ultrafast和完整QQ模块；纳入与最新QQ测试站manifest一致的原图直传、content和安全错误诊断修复。审查额外修复Gemini native下游写失败、SSE分隔符失败和清洁EOF尾data的图片交付计数，保留token用量与已送达图片。

本地最终unit59包25,714个PASS事件/0失败/18外部条件skip；QQ真实PG483PASS/0skip；305→306升级、fresh install、幂等与二开数据/结构保全通过；前端442文件3961项/类型/lint/build、Linux lint0 issues、四包vet和独立审查通过。Go安全0可达漏洞、2个未调用模块漏洞；前端保留既有xlsx高危例外，未增加豁免。Windows通用发布工具的POSIX条件测试由精确源码Linux CI覆盖。详见[完整合并审查](../docs/changes/2026-10-26-upstream-ac1b8f1.md)。

## 升级注意

1. 备份数据库、Compose、应用环境和配置；保留现有密钥、数据卷和回退镜像。
2. 新增 `246_enable_gemini_image_generation_groups.sql` 会把已有Gemini/Antigravity分组的生图开关false改为true；Composite/OpenAI不变。升级前核对策略，需禁用时升级后在后台关闭，后续启动不会再次强制开启。
3. 两个246迁移按完整filename区分，305→306已验证。不要重命名旧SQL或改checksum。
4. 从r4及更早版本升级还涉及QQ的248–250迁移；首次正式启用QQ须遵循[r5升级准备与资金回退说明](CUSTOM_IMAGE_0.2.17-r5.md)，保留独立加密主密钥，避免正式/测试实例同时处理同一QQ收费指令。不要恢复旧数据库覆盖新订单和余额。
5. 发布镜像不等于部署：本任务未更新正式站、QQ测试站、Cloudflare或检测站，也未调用收费模型/发送QQ。

## 发布结果

尚待精确源码CI/安全门禁、双架构构建和匿名registry/provenance校验。完成后在此补全Actions链接、控制SHA、固定digest和拉取命令。
