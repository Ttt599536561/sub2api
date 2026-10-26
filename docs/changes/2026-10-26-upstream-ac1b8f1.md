# 2026-10-26 upstream ac1b8f1 合并与 Linux 发布审查

## 来源与范围

- 上游固定 `ac1b8f1271be0d674fa6f074403c1410f30a3a81`（0.2.17）。拉取时上游main已经更晚，本次按用户明确提交，不扩到后续提交。
- 本仓基线 `cda70a59b584932d3de120423112a9b4feb707e5`，含公开r5及完整二开功能；共同祖先 `060d0160417782cd962a0c7f460f6cd4565d4916`。
- 纳入最新QQ测试站原图投递修复的11份源码/测试，逐文件SHA256与已部署inline快照 `74148a5aae1ec83aeae7d5213e20996ba9ff1f2810da45b87c2c576b460ccc96`一致。详见[已部署修复](2026-10-26-qq-image-delivery.md)。
- 主目录其余Codex客户端版本/CCSwitch以及本机环境/历史草案WIP保全，不计入本次发布源码。已完成AI定位文档一并归档。
- 使用E盘独立工作树，主目录原34份改动/非忽略文件已备份并记录指纹。缓存与依赖复用E盘，不使用本机Docker构建。

## 合并处置

上游差异涉及114个文件，6个文本冲突：

1. chat-completions/responses handler保留上游部分流失败后的用量提交，同时保留二开渠道字段及推理档位；异步入队前同步快照gin.Context字段，避免请求复用竞态。
2. Antigravity流转换保留二开增量usageMetadata及延迟finish机制，叠加上游负输入token归零。
3. 计费测试保留双方导入及断言；上游已删账号测试补齐本仓福利累计SQL预期，不移除二开结算。
4. package/lock保留固定DOMPurify3.4.17、follow-redirects1.16.1，后端x/image0.45.0和Go1.27.3不降级。

合并前QQ基线测试发现15ms ACK时序触发模拟网关重连、重发事件、重复关闭channel；改为业务进入后按协议主动请求三次心跳，意外断线明确失败。生产网关不变，保留最新序号与取消join断言；原夹具45ms延迟必现panic，修后100次正常及10次同延迟场景均通过。

## 审查发现并修复的计费边界

Gemini原生流原先忽略客户端写失败，并在写成功前登记图片和结束状态；完全失败的writer仍计1张图片。独立复核还发现data行成功但SSE分隔空行失败的相同问题。现已观察的上游token仍按原规则记录，图片与finish仅在完整事件成功写出后登记，之前已成功图片与累计图数max语义保留。OAuth一次写完整事件的路径保留。

为兼容上游清洁EOF尾data，仅正常EOF时补齐缺失的1或2个换行，补写成功后登记；取消、非EOF读错和下游失败不补帧、不登记pending图片。永久11叶回归与独立overlay11叶均通过，涵盖inline/file图片、数据行/空行/EOF补写失败、尾comment、已送达图片保留与多图max；原缺陷均先RED再GREEN。此次不更改用户余额公式或上游token计费策略。

QQ已上线投递测试中的两个未检查类型断言按lint要求改为明确验证Flusher；生产QQ代码与已部署修复保持同一逻辑。

## 数据库升级注意

新增 `246_enable_gemini_image_generation_groups.sql`，将已有gemini/antigravity分组的 `allow_image_generation=false` 改为true，composite/openai不改。升级前应核对这些分组的生图策略；升级后如需禁用，可在后台重新关闭。此为上游明确行为，不默默改写其迁移。

它与二开 `246_group_force_openai_ultrafast.sql` 数字前缀相同，但迁移器按完整filename和checksum识别；保留两者原名及所有已发布SQL，禁止重命名历史迁移。r5完整基线305份SQL升级后为306份。QQ正式启用准备与历史资金回退边界仍按[r5升级说明](../../deploy/CUSTOM_IMAGE_0.2.17-r5.md)。

## 实际验证与发布边界

本次验证过程记录于主项目 `.cache/merge-ac1b8f1-20261026`。前端完整442文件3961项、typecheck、lint、build通过；使用与CI一致的pnpm9.15.9，局部PATH解决全局pnpm12不兼容，不改全局配置。后端Linux目标govulncheck为0可达漏洞，另有2个未调用模块漏洞；前端门禁保留既有xlsx两项high例外，未新增豁免，不宣称依赖零漏洞。

真实PG16.15 QQ模块483个PASS事件、0失败/0跳过；305→306升级与全新安装、旧checksum/applied_at、余额冻结、推广域/QQ代表数据和结构保全、重复启动均通过，删除新增SQL的负向控制正确失败。Linux目标lint零问题，四包vet与调度/配额/福利/流式独立审查通过。最终源码整仓unit：59包、25,714个PASS事件、0失败、18项外部服务/环境条件跳过；本轮真实PG项目不跳过。Linux目标最终lint为0 issues，源码4611文件指纹与验证快照一致，前端1064文件亦与此前验证一致。4项部署脚本检查、Apple脚本仅语法检查及本机Compose配置16组合检查通过。通用release-matrix的Windows本机执行受POSIX可执行位/路径条件限制，交由精确源码Linux CI验证；没有运行macOS构建。独立审查无剩余阻塞，准备合入主目录并推送。GitHub CI/安全门禁成功后才发布固定源码 `0.2.17-r6`，发布记录另列精确源码、控制提交、Actions与digest。

本任务不部署生产/测试站、不改变用户数据、不调用收费模型或发送QQ。镜像发布与服务器实际运行版本分开记录。
