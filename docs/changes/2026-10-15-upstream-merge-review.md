# 2026-10-15 官方最新代码合并与复审

合并源码提交：`5fc3298e74a9c6423ce0463ec2eb5c9b59f6c584`（两个父提交分别为下述二开和官方输入）。本记录随后补充实际 SHA，不代表镜像发布。

## 来源与交付边界

- 二开输入：`11fe558f0b47862c64e75aed172277d6e6c624c6`，包含远端 `origin/main` 的 `570e2e00d733ebb20d6692c394c21f05101d0381`（已发布 `0.2.15-r1`）以及交接文档。
- 官方输入：本轮拉取固定的 `698d142468ab15374bc22bd5247759e5b7352006`，包含官方 `v0.2.16`（`01842576b3d5c1e2545b5a99ad555d36e28ed306`）及其后主分支更新。相对上次官方基线 `3a6fd1c9d` 增加 103 个提交，变更 283 个文件。
- 分支：`codex/merge-upstream-20261015`；工作树：`E:\sub2二次开发项目\.worktrees\sub2api-upstream-20261015`。本轮合并与修复在该独立工作树完成。
- 主目录原有 Codex 客户端身份、CC Switch 导入、环境脚本、部署指南及未跟踪文件未纳入本轮合并。工作树没有复制这些未提交修改。
- 本轮为本地源码整合；不代表镜像发布或生产部署。已发布版本仍为 `0.2.15-r1`，线上实际部署未连接核实。

## 文本冲突与生成代码

Git 报告 3 个文本冲突，均逐项整合：

1. `backend/internal/service/billing_cache_service.go`：保留二开的 `RevalidateSubscription`，同时合入官方固定费用预留及 API Key 窗口检查。
2. `frontend/src/components/account/ModelWhitelistSelector.vue`：保留 `isCurrent()` 会话归属检查，采用官方 API 错误信息提取。
3. `backend/cmd/server/wire_gen.go`：从合并后的 provider 重新生成，保留福利 outbox 清理并接入 Gemini 缓存服务清理。

额外发现 Ent 自动合并未报文本冲突但字段序号已不一致：新增 `i18n` 后，分组 runtime 默认值和 `group_sort_order` 索引仍引用旧位置。按合并后的 schema 重新生成 `migrate/schema.go`、`mutation.go`、`runtime/runtime.go`，纠正索引并保留日重置字段。

## 复审修复

- 固定费用不足判断对仍为正数的偏低余额缓存做独立数据库回读，避免福利兑换或退款后的迟到扣减造成错误拒绝；不绕过余额 generation fence 或原子在途预留。
- 固定费用在登记订阅 scope 之前重新读取权威分组/订阅，拒绝订阅身份或计费模式变化，并使用新额度；避免分组限额下调后超额放行、上调后误拒及新限额未登记预留。
- Gemini 缓存创建在取得账号并发槽后、调用上游前补权威订阅终检；不重复累计 RPM。
- 缓存创建断线后仍需完成上游创建、清理和结算；用户及账号并发槽不随客户端取消提前释放，并保留幂等释放与异常清理。
- 上游缓存已创建但本地保存失败时，清理上游资源并结算已发生的输入 token 费用，不收缓存存储费用。
- 运维账号名称缓存按登录会话和用户分区；分页与请求明细携带请求归属，在切号、权限变化、关闭或卸载后拒绝旧回包回填或继续翻页。

- 缓存续期一次性确定绝对截止时间，预留、上游 PATCH 的 expireTime 和最终计费共用该时间，避免网络延迟把缩短操作变成收费延长，或让实际费用超过已预留金额。

- 日额度重置、自动开关、状态查询与操作重放响应按 Accept-Language 本地化分组，同时去掉译文表并保留服务层对象及二开元数据。此遗漏由首轮全量单元测试检出，专项修复后通过。

## 二开能力与迁移

月卡日额度提前重置、每次扣 86400 秒、每日 100 次上限、自动偏好默认关闭、周/月用量保留、支付/退款恢复、福利原子结算和管理员统计保持原规则。固定缓存费用走已有原子余额结算与福利累计链路；订阅额度消费不累计账户余额消费抽奖。

原有 296 份 SQL 内容未修改。本轮仅新增：

- `242_group_i18n.sql`
- `243_gemini_cached_contents.sql`
- `244_gemini_cached_contents_channel_usage.sql`
- `245_gemini_cached_contents_mutation_lease.sql`

历史升级回归的旧快照需显式增加新 `i18n: {}` 默认列：仍对完整实际行做严格相等比较，没有忽略新列或放宽旧数据断言；同时保留历史文件 checksum 与重复启动验证。

迁移器按完整文件名记录和校验，不按数字前缀去重；既有 `242_*` 文件保留。后续部署前应按既有发布流程备份数据库，不能用本次源码验证代替线上备份与升级确认。

## 验证

| 检查 | 实际结果 |
| --- | --- |
| 前端 `corepack pnpm@9.15.9 test:run` | 420 个测试文件、3508 项测试全部通过。 |
| 前端 `lint:check`、类型检查、`build` | 全部通过；build 包含 i18n 完整性检查、vue-tsc 和 Vite。有大于 500 kB 的产物提示，不影响构建退出状态。 |
| 后端 `go test -p 2 -count=1 -timeout=20m -tags=unit -json ./...` | 最终重跑退出 0；58 个有测试包，23,678 个通过事件、18 个条件跳过、0 失败。包含父测试与子测试事件，不等同独立用例总数。 |
| 后端 `go test -p 2 -count=1 -timeout=20m -tags=integration -json ./...` | 最终重跑退出 0；52 个有测试包，14,498 个通过事件、19 个条件跳过、0 失败；三条历史升级路径均通过。 |
| `go test -p 2 -count=1 -tags='unit,embed' ./internal/web ./cmd/server` | 最终源码两个包通过。 |
| `go build -p 2 -tags embed -trimpath ... ./cmd/server` 与 `-version` | Windows/amd64 嵌入前端构建、版本启动检查通过，显示 `0.2.16-merge-review`。 |
| Wire / Ent 生成、差异检查 | 按合并 schema/provider 成功生成；产品 `go.mod/go.sum` 未变；`git diff --check` 无错误。 |
| 原工作区保留 | 主目录 HEAD 仍为 `11fe558f0`；前后 tracked diff 的 SHA-256 一致，`git status --porcelain` 列表一致。 |

首轮全量 unit 发现日重置响应未适配多语言的 1 项失败，修复后新增 4 个语言子场景并通过最终全量重跑。集成首轮的三条历史升级快照因合法新增 i18n 列失败，显式校验新列并保留完整旧数据断言后，最终全量重跑通过。unit 与 integration 事件有重叠，不相加为独立用例数。

基线：前端模型选择器 25 项、二开会话/福利/订阅专项 517 项通过。后端 service、handler、server 基线通过；repository 首次因进程缺少 `sh` 的 3 个备份用例失败，将现有 Git Bash 加入该进程 PATH 后完整重跑通过（11.507 秒）。没有为环境错误修改产品源码。

前端会话修复：18 项新增回归在修复前按预期失败，修复后专项 5 文件 44 项通过；类型检查通过。后端修复使用对应回归先复现再验证。

构建脚本内层 `pnpm` 最初落到本机 11.x shim，导致依赖配置版本检查失败；仅在本次进程 PATH 使用 E 盘临时 9.15.9 启动器后通过，没有修改项目依赖或全局设置。Wire/Ent 工具依赖使用 E 盘临时 modfile 副本，生成串行完成，避免 Windows 文件映射锁。

环境：Windows amd64、Go 1.27.2、pnpm 9.15.9；每个命令点载主项目 `tools/project-env.ps1`，依赖、编译与临时缓存均在主项目 `.cache`。Docker 设置及实际 VHD 位于 `E:\CodexData\DockerBackup\DockerDesktopWSL\disk\docker_data.vhdx`，复用已有 PostgreSQL 18.1、Redis 8.4 和 Ryuk 镜像。测试镜像版本不改变生产数据库版本。

单元测试的 18 项条件跳过包括既有钉钉 OAuth 占位、外部 TLS/官方 CLI 抓包（Windows 不适用）、需显式 Redis/PG 地址的安全审计测试、SQLite 无法构造的历史 NULL 场景，以及真实 TypeSafe/OpenAI 和插件运行时。未调用生产支付、OAuth 或模型 API；本机 Windows 构建不代表 Linux/arm64 或发布镜像已验证。

集成全量中的 3 项 Redis 限流测试因旧探测只检查 Unix socket 而在 Windows 跳过；随后为单独进程设置已核实的 Docker Desktop npipe 端点，补跑这 3 项全部通过（`backend-redis-windows-supplement.log`）。因此全量日志保留 19 项 skip 的真实记录，追加验证后仍未执行的为 16 项条件场景；包括既有禁用的并发缓存子用例以及外部服务/抓包/安全审计显式环境等。

最终复审未发现本轮范围内尚未解决的 P1/P2 问题；自动测试不能替代真实外部服务和生产部署验证。

原始验证日志位于主项目 `.cache/merge-20261015`；本文件和更新日志是随 Git 保存的交接摘要。
