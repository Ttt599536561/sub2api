# 1cfafd30e 全面复审与修复

- 日期：2026-10-18（Asia/Shanghai）。
- 固定应用基线：`1cfafd30e83e52d5980e2be4c7b78a80e797a797`，已发布镜像 `0.2.17-r1` 的源码。
- 修复源码与回归提交：`19b66b0efa2fff5006d512693524c50ff151a0ae`（本地，未推送）；后续仅补录真实提交引用，不改变已验证应用代码。
- 开始工作时工作树 HEAD：`f5374ecd4bdb0848f802e650a3ee4d4319118f41`。它与指定基线的差异仅为 CI/发布文档，应用代码一致。
- 工作位置：`E:\sub2二次开发项目\.worktrees\sub2api-upstream-v0.2.17-20261018`；主目录原分支与既有未提交修改保留。
- 状态：九类问题修复、永久回归和最终验证完成。源码、测试与记录按稳定主题 `fix: align post-merge model routing and refund recovery` 同一提交保存；未推送、未发布新镜像、未部署生产。

## 审查范围

检查目标提交、v0.2.17 合并增量、此前 Ultrafast/cyber 二开接线及关键恢复路径；并行审查退款与订阅恢复、模型路由/目录/余额预占、前端支付与管理页面，主代理复核支付提供商、Gemini 兼容与整体集成。Git 未合并索引为空；冲突标记扫描的等号字符串是既有提示词分隔线，不是遗留冲突。问题主要属于语义错位和边界恢复，而不是残留的文本冲突。

## 已复现的问题与修复

1. **支付宝退款结果分类（P1）**：带签名的业务失败可能由 SDK 返回响应体与空 error；旧代码把明确拒绝当作 pending。另一方面，网络/解码失败会丢掉已经生成的退款请求号，后续可能重复退款。明确拒绝按已核对的业务子码返回失败；系统错误、未知码、空响应及传输错误保留原请求号并返回 pending，后续只查单。`ACQ.SYSTEM_ERROR` 不能作为明确失败，依据[支付宝退款接口说明](https://aipay.alipay.com/docs/subscribe/api-list/other-basic-interfaces/alipay-trade-refund.html)。
2. **退款审计顺序（P1）**：按各应用实例生成的 created_at 取最新记录，时钟偏差/回拨会使旧未归还标记覆盖新已归还事实，或使用旧 pending 的扣减金额。回归复现成功退款未扣余额、失败恢复余额从应有 100 变成 140；改为按同订单串行事务产生的审计 ID 排序，保留 legacy 的 ID 边界。
3. **退款准备后的用户申请（P2）**：管理员先准备退款、用户随后申请退款、执行再失败时，旧计划把订单恢复成 COMPLETED，覆盖已接受的 REFUND_REQUESTED。认领成功后保存实际认领前订单快照，失败回到该状态。
4. **首次 pending 审计瞬时故障（P1）**：网关受理后第一次本地持久化失败会留下 REFUNDING。使用原扣减和同一退款号，在新的有界事务中重试一次本地快照；不归还扣减、不再次调用网关。覆盖余额及撤销订阅、后续成功及失败。
5. **HTTP/WS 模型规则与余额预占（P1）**：Messages/Chat 与 native WS 的实际账户映射仍被预占计算当成 HTTP passthrough；WS handler 也在预占之后才设置传输 context。按请求实际模型语义估算，并提前设置 WS context。真实 WS 回归证明实际发送 Ultrafast 时旧预占为 0.03081，而应为 0.24648。
6. **模型冷却与实际转发模型（P2）**：选号冷却读取仍使用旧 HTTP 解析规则，跨 Messages/native WS/passthrough WS 可重复选择已处于冷却的目标。读取贯穿 legacy、advanced、sticky、previous-response 与数据库重检，统一采用请求 context；原生 WS 建连失败也按已经解析的实际出站模型写入冷却，避免写端继续使用 HTTP 规则。
7. **模型目录渠道投影（P2）**：目录先按公开名执行账户白名单，导致渠道已映射到可用目标的别名提前丢失。远端投影和显式分组白名单的配置目录都先执行渠道映射再执行账户映射，保留公开候选名与最终目标元数据，不修改共享原始缓存。
8. **支付页面路由归属（P2）**：Vue Router 复用同一支付页面时，旧 Stripe secret/二维码可与新订单查询或成功回跳混用；Airwallex 可把旧 intent 与新 successUrl 组合。固定初始支付 query 身份，变化后同步失效并复用原会话清理；旧迟到结果以及同一组件内支付 query 改变后又改回，都不能恢复旧操作。

9. **Messages 分组默认值与账户映射优先级（P2）**：选号按分组默认模型判断退役，实际转发优先使用账户对原始/渠道模型的映射，造成退休目标漏拦及有效目标误拒。请求 context 分别保存渠道输入与分组默认值，准入、冷却、预占按与实际转发相同的账户优先顺序解析；默认值不再经过第二次账户映射。12 个真实 handler 场景中，原代码 8 个失败、4 个控制通过。

## 验证

所有项目命令在同次 PowerShell 先加载主项目 `tools/project-env.ps1`；复用 Go 1.27.2、pnpm 9.15.9 与 E 盘共享缓存。以下均为本轮实际结果，不沿用旧报告数字。证据位于主项目 `.cache/review-1cfafd30e-20261018`，本页保留可跨机器交接的结论。

| 范围 | 实际结果 |
| --- | --- |
| 新增永久回归 | 75 个叶测试通过：后端 62（路由 39、退款 12、支付宝 11），前端 13；父测试通过事件不重复计为叶测试。 |
| 后端全仓 unit | `go test -p=1 -vet=off -tags=unit -count=1 -timeout=20m -json ./...` 退出 0；58 个测试包、24,541 个通过事件、22,410 个叶测试，0 失败、18 条件跳过。测试进程 PATH 加入已有 Git `sh`。govet 由下一项静态检查覆盖。 |
| CI 标准 Go 静态检查 | golangci-lint 2.14.0，`run --concurrency=2 --timeout=30m ./...` 退出 0，`0 issues`，包括 govet、staticcheck、gosec 等项目既定规则。 |
| PostgreSQL 仓储 integration | `CI=true go test -p=1 -vet=off -tags=integration -count=1 -timeout=25m -json ./internal/repository` 退出 0；1,670 个通过事件、1,544 个叶测试，0 失败、1 既有跳过。退款原子恢复、订阅撤销、福利退款及三条历史升级均通过。首次缺少 sh 的 3 项环境失败在补进程 PATH 后完整重跑通过。 |
| 前端专项与静态检查 | 最终支付 3 文件 / 44 项通过，vue-tsc、全库 ESLint 与 4 改动文件 ESLint 均退出 0。 |
| 前端全量 | `corepack pnpm@9.15.9 exec vitest run --maxWorkers=2 --minWorkers=1 --no-cache`（JSON reporter）退出 0；424 文件、3,637 项全部通过，0 失败、0 跳过。最终前端文件哈希与冻结快照一致。 |
| 前端生产构建 | `corepack pnpm@9.15.9 exec vite build --outDir <本轮缓存>/frontend-build` 退出 0；183 个文件、6,385,925 字节。仅既有 Browserslist 数据和大 chunk 提示。 |
| Linux 编译 | `GOMAXPROCS=2 CGO_ENABLED=0 GOOS=linux GOARCH=<arch> go build -p=1 -o <本轮缓存>/build/sub2api-linux-<arch> ./cmd/server`，amd64/arm64 均退出 0；ELF 架构、大小和 SHA256 经主代理复核。仅编译，不代表镜像发布或生产启动。 |
| 源码与文档 | 22 个改动 Go 文件 gofmt 检查、diff --check 通过；26 个代码/测试文件哈希复核无生产漂移。82 个本地链接、70 个提交引用有效。 |
| 独立复核 | 支付提供商、退款、前端、路由和报告交叉复核通过；复核补出的未知退款语义、Messages 默认映射及 WS 建连冷却写端均补测试后修复。 |

18 项 unit 条件跳过包括 9 项需专用安全审计 PostgreSQL/Redis 环境变量的测试、3 项外网 TLS 指纹测试、1 项 Linux 抓包对照、钉钉 OAuth 占位、SQLite 不支持的旧 NULL 行、TypeSafe live、真实 OpenAI token 对照及可选插件运行时。仓储 integration 的 1 项跳过为已有并发缓存批量读取 TODO。没有使用生产凭据；本轮真实容器是 PostgreSQL 18.1/Redis 8.4，不代表生产数据库版本。Docker 实际 VHDX 及父目录再次核实在 E 盘且无联接；测试容器已自动退出。

额外增加 `--build-tags=unit` 的全库 lint 首轮报 85 项：本轮新增 WS 测试的 CloseNow 返回值处理已修正，并重跑其 4 个叶场景通过；另 84 项位于与 `1cfafd30e` 完全相同的旧测试文件，属于扩展标签下的 depguard/errcheck/govet/style/unused 告警，未在本次业务修复中批量改写。该额外口径不标为全绿，项目 CI 标准口径已实际通过。相关逐文件核对保存在 `extended-unit-lint-audit.json`。

原始失败复现和最终日志分开保存。早期 WS 夹具漏放认证 context 的分组，以及退款夹具已经包含一条历史 pending 审计的计数偏差，均修正夹具后重新验证，未当成业务失败或通过依据。

前端临时构建目录和两份 Linux 二进制的删除均被自动审批以 `blocked by policy` 拒绝，未给出具体规则；产物原位保留，没有更换工具或重试绕过。前端摘要在 `frontend-build-summary.json`，二进制摘要在 `build/summary.json`。二进制实际核验值：

- amd64：161,082,887 字节，SHA256 `0cbb41492d727f77818150106de175e442ea0b5e551755835aeb65222993627d`。
- arm64：150,121,128 字节，SHA256 `f8ac01b1b2f3e8ce84247c903e28b5f5826ddac0f3ce70494a8f81bd79c0a164`。

## 部署与恢复边界

无新增 SQL/Ent schema/依赖/部署配置。当前发布镜像仍为原 `0.2.17-r1`；本次修改不会自动进入既有镜像。新代码保留原退款审计格式和既有 pending 查询契约。持续数据库不可写或进程在首次持久化前终止仍可能留下需要人工核对的 REFUNDING；本轮有界重试处理瞬时故障，不能把未知网关结果自动当作失败或自动重发。未访问真实支付/模型接口或生产数据。
