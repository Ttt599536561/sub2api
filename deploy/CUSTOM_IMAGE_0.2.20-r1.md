# Sub2API 二开 0.2.20-r1 Linux 镜像

2026-11-07 已发布到GHCR，仅包含linux/amd64和linux/arm64。服务器可在Compose的`image`字段使用：

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.20-r1
```

固定内容地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:9b924439538ee43b183cbfddf7b729f65f79476c71879b69bda199e133dde583
```

## 来源与验证

应用固定源码 `2f398e371e58ed7375f605b0d71f44d515fd6742`，发布控制 `13b832682ca69247703510187b7d413f0e993758`。本地主目录与GitHub main均已整合，原23项WIP、5份其他未跟踪文件与293份dist保全且不纳入镜像。后续记录提交只更新Markdown。用户自行切换服务器，本轮没有部署服务器。

[专用Linux工作流](../.github/workflows/publish-custom-v0220-r1.yml)固定上述应用SHA，不调用macOS、Windows或GoReleaser构建。

| 检查 | 结果 |
| --- | --- |
| 精确应用门禁 | [CI 42371095920](https://github.com/Ttt599536561/sub2api/actions/runs/42371095920)及[Security 42371095918](https://github.com/Ttt599536561/sub2api/actions/runs/42371095918)成功。 |
| 镜像发布 | [发布42372863369](https://github.com/Ttt599536561/sub2api/actions/runs/42372863369)全部成功。两个原生Ubuntu runner分别构建、拉取和实际运行镜像；版本/源码/Go1.27.3、pg_dump/psql18.6、价格资源、安装状态和嵌入前端检查通过。 |
| 验证产物 | 5份必须artifact ZIP的大小、SHA256和内容与GitHub元数据一致，二进制源码与版本绑定通过。 |
| 匿名拉取 | 北京时间2026-11-07 20:40:14，最终index、运行manifest/config及全部23个去重运行层/92,222,580字节完成匿名摘要与大小校验，tag前后稳定。 |
| 独立终审 | 验证artifact、运行记录及匿名实时manifest/config均一致，仅Linux两个架构，无确认缺陷；没有重复下载运行层。 |
| 发布控制检查 | [Security 42372840186](https://github.com/Ttt599536561/sub2api/actions/runs/42372840186)成功；[CI 42372840227](https://github.com/Ttt599536561/sub2api/actions/runs/42372840227)两轮分别在unit/integration中的同一分配计数测试失败（29/52）；已停止重试并实施下述测试隔离修正，最终测试维护 `4ff02d1d4fae06e61e6379593933c8b5df07d72e` 的[CI 42376322283](https://github.com/Ttt599536561/sub2api/actions/runs/42376322283)与[Security 42376322331](https://github.com/Ttt599536561/sub2api/actions/runs/42376322331)均已成功。 |

| 平台 | Runtime manifest |
| --- | --- |
| linux/amd64 | `sha256:4ac179db16da994169151141599a38d748b23d9d71d11d2a0b0b218ac0a98d47` |
| linux/arm64 | `sha256:7bbe548f379349677ae1ba7f6da79ff5b417e079e72df900072e9edc22a67e28` |

## 控制CI首轮失败与复验

控制提交相对应用提交仅增加工作流和文档，业务源码与测试代码完全相同。首轮`TestOpenAIResponsesOrphanPrecheckLargeEscapedContent`的`testing.AllocsPerRun(5)`断言期望0、观察到29，其余测试没有报告失败。

没有修改业务代码或放宽原断言。原测试分别在Windows/amd64与Linux/amd64隔离重复100次，均为0分配；独立cache-only实验10次均证明：同一零分配函数旁由另一个goroutine每轮分配29次，进程级统计会得到29。这支持计数受后台工作干扰的机制，但不能证明CI当时具体哪个协程贡献了那些分配。保留两轮日志及实验记录，仅重试失败job一次：第二轮unit通过，但integration同一断言再次观察到52分配，随后停止重试。发布镜像始终来自已经通过精确应用门禁的2f398e371，未因重试改标签或替换内容。

### 分配测试隔离维护

仅对 `openai_responses_orphan_precheck_test.go` 增加子进程包装：精确选择当前测试、专用环境标识防递归、父30秒/子25秒超时并传回失败输出。原输入、5次测量与零分配断言逐字保留，没有改业务函数或放宽阈值。受控旧测量在背景噪声下3次均失败；修后目标30次和活跃父噪声10次均通过；子进程中每次真实增加1个堆分配时，仍按预期失败并传播给父测试。integration标签相关测试重复5轮通过，独立终审通过。

此维护不修改任何生产源码、依赖、镜像工作流或已发布标签；运行镜像仍严格绑定已通过应用门禁的2f398e371。完整service复验通过：unit 19,483个PASS事件、10个条件跳过；integration 9,631个PASS事件、3个条件跳过，均0失败。

首个测试维护提交 `0ebc863f33dcc97231dc5656091dcb85eb344ad1` 的[CI 42375396772](https://github.com/Ttt599536561/sub2api/actions/runs/42375396772)发现G702：`os.Args[0]`作为子进程路径被视为不可信输入。最小修正改用`os.Executable()`并检查错误，未添加扫描豁免。Windows宿主上的Linux目标service完整lint明确RED 1 issue→GREEN 0 issues，目标5次和独立复验通过。该修正提交 `4ff02d1d4fae06e61e6379593933c8b5df07d72e` 的GitHub全仓CI和Security均成功，包含Linux完整unit/integration、frontend、lint和发布辅助检查。旧0eb的完整测试任务本身也已通过，G702失败保留为历史。不能把旧控制13的两次失败写成成功；此处记录的是修正后的新提交通过。

## 从0.2.19-r2升级

- 首次启动自动增加 `249_mcp_oauth.sql`，迁移310→311；保留旧SQL校验值及QQ同号完整文件名。该迁移创建三张MCP OAuth表，不重置用户余额、QQ订单/授权、福利或推广数据。升级前备份数据库和配置。
- 沿用现有QQ加密密钥和接收器设置，保留 `QQ_IMAGE_EXTERNAL_HELP=true`，测试收费接收器继续关闭。独立CF/智商检测站不随本镜像更新。
- MCP默认关闭，不必为此次升级新增 `MCP_PUBLIC_URL`。如之后需要启用，按[管理MCP说明](../docs/mcp.md)配置。
- auth snapshot v26→v28，旧缓存自动重新加载，无须清空Redis或重建API Key；共享Redis的多节点应协调升级，减少新旧快照反复覆盖。
- 上游新增实例请求体缓冲预算默认1GiB，以及读/写/flush单次操作超时120秒，均可配置；不是SSE总时长限制，详见[资源与边缘安全](EDGE_SECURITY.md)。客户端断开后的上游读取计费用量窗口有上限。
- Ultrafast文本Standard×8及媒体预占/计费例外保留。GPT-6.1 Terra的OAuth/setup-token订阅请求扩展长上下文附加费豁免，API Key原价卡保留。


## 本机证据

关键结论保存在本文件。原始记录位于 `.cache/publish-v0220-r1-20261107`，已验证产物与registry摘要位于 `output/publish-v0.2.20-r1`；共享依赖/镜像缓存与主目录恢复备份保留。发布和验证没有触发真实QQ/邮件/收费模型调用。

本轮Linux隔离诊断容器已自动删除；临时 `alloc-linux.test` 删除命令在执行前被自动审批以 `blocked by policy` 拒绝，未提供具体原因，未重试。该二进制留在本轮E盘缓存，证据日志和共享编译缓存保留；不影响已发布镜像。

最终交付前于北京时间2026-11-07 21:32:26再次匿名检查版本tag，HTTP200且digest仍为`sha256:9b924439538ee43b183cbfddf7b729f65f79476c71879b69bda199e133dde583`。仅测试与文档维护没有覆盖或重新发布镜像。
