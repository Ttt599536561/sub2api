# Sub2API 二开 0.2.17-r6 Linux 发布与升级记录

日期：2026-10-26（Asia/Shanghai）。**镜像已发布，双架构运行检查与匿名完整运行层校验通过。** 本任务未部署生产或QQ测试站。

## 固定来源

**上游版本身份更正（2026-10-26）**：官方 [v0.2.18](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.18) 的标签对象`de86961b627e48db75d61fff8c05535d44a00cbe`指向提交`ac1b8f1271be0d674fa6f074403c1410f30a3a81`，该提交已包含在本镜像源码祖先中。因此本镜像已经合入官方0.2.18发布基线。文件名和镜像中的`0.2.17-r6/r7`属于沿用的二开发行命名；此前将该上游提交标为0.2.17的说明有误。上游标签提交里的VERSION文件仍为0.2.17，官方发布脚本会按tag改写构建工作区版本，本仓发布则显式注入二开版本号。已发布镜像地址、二进制和digest保持不变，本次仅纠正文档。

- 上游：官方v0.2.18对应的`ac1b8f1271be0d674fa6f074403c1410f30a3a81`，按用户明确提交合并，不扩到后续上游main。
- 合并提交：`787d5a4a3d5ad8723b5a83b392c83ead888962cb`，父提交为本仓cda70a59b与指定上游ac1b8f1。
- 最终应用源码 / OCI revision：`02f706a193a1b5c79cd43c4ec5aed85a12553293`。合并后的两次修正仅调整容器迁移与福利集成测试，应用业务代码未改变。
- 发布控制提交：`69b52de91a59472f54694b131f013363c818ba06`，仅更新发布配置/文档。[专用工作流](../.github/workflows/publish-custom-v0217-r6.yml)固定上述应用SHA，要求同SHA最新main/push CI和Security Scan成功，禁止覆盖已有标签。
- 镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.17-r6`，只包含 `linux/amd64` 和 `linux/arm64` 两个运行平台；原生Ubuntu构建，没有macOS任务。
- 最终index digest：`sha256:e19bb55853094c3da1df7649b071b8235bba4fcc2164ca7f87c7f463d51155f1`。
- 主目录原Codex/CCSwitch等11份WIP及既有非构建untracked内容保留，不属于镜像快照。保护引用、逐文件备份和恢复后指纹均保留；本机恢复后的Codex专项937PASS事件、CCSwitch25项通过。

## 改动与审查

合并上游调度快照、账号配额、流式断连计费、Gemini分组生图开关等改动，保留福利、推广、监控、ultrafast及完整QQ模块；纳入与最新QQ测试站manifest一致的11份原图直传/content/安全错误诊断源码和测试。

6个冲突逐项合并，保留异步用量任务的请求字段快照、Antigravity增量usage与延迟finish、福利结算和既有安全依赖固定。额外修复Gemini native下游写失败、SSE结束分隔符失败及清洁EOF尾data的图片交付计数；token用量保留，之前完整交付的图片与max计数不丢失。修复QQ测试夹具的心跳时序和类型断言。详见[完整审查与各阶段证据](../docs/changes/2026-10-26-upstream-ac1b8f1.md)。

本地合并阶段完整unit为59包、25,714个PASS事件、0失败、18外部条件skip；前端442文件3961项/类型/lint/build通过，QQ真实PG483PASS/0skip，305→306迁移升级/fresh/幂等/资金及二开数据保全通过。后续测试兼容修正分别通过真实PG专项和完整repository integration（1788PASS事件/3skip），最后明确Gemini ServiceAccount夹具后福利专项19PASS/0skip及integration vet通过。Linux lint0 issues、独立审查通过。各阶段数字不冒充同一次运行，最终固定SHA的完整Linux CI另见下表。

安全门禁：Go无可达漏洞，提示2个未调用模块漏洞；前端保留已有xlsx两项high例外，未增加豁免，不宣称依赖零漏洞。

## 发布验证

| 项目 | 已核验结果 |
| --- | --- |
| 精确源码CI | [40303821798](https://github.com/Ttt599536561/sub2api/actions/runs/40303821798)，5个job全部success，包含unit、integration、真实PG迁移、前端、lint及发布辅助。 |
| 精确源码安全扫描 | [40303821916](https://github.com/Ttt599536561/sub2api/actions/runs/40303821916)，全部success。 |
| 双架构发布 | [40305430958](https://github.com/Ttt599536561/sub2api/actions/runs/40305430958)，attempt1的gate、amd64、arm64、publish均success。 |
| 实际二进制与运行检查 | 两架构r6版本、完整源码SHA、Go1.27.3、x/image0.45.0相符；psql/pg_dump、价格资源、setup状态和嵌入前端检查通过。 |
| 工作流产物 | 5份ZIP的SHA256与GitHub元数据一致，run/control/source绑定通过。 |
| 匿名registry | index/runtime manifest/config/platform/revision/version及架构artifact绑定通过；24个运行层、90,204,030字节SHA256/大小逐个通过；末尾重读标签仍一致。 |

匿名核验完成时间：北京时间 **2026-10-26 18:19:25**。运行层通过HTTP流式校验，没有向本机Docker引擎拉取本次发布镜像。未验证attestation内容或签名；实际运行验证由GitHub各架构任务完成。

| 平台 | Runtime manifest | OCI config |
| --- | --- | --- |
| `linux/amd64` | `sha256:11d9bf93f68186e70fbdf15fc9108863ec4167404ee8ad886bea51f36f09c5ec` | `sha256:86c105bf947776877f3062ab931d26358964a6e702c5faa15129d2af8e14575b` |
| `linux/arm64` | `sha256:184fe3adf3487a325d4ce1e54e2441f2b5d8c1bdbd32c785d61197cb0e210a2e` | `sha256:0d7794b0932921314ddbe5644e34deb69b3e51d13cba5d0598a535eb79d98587` |

```bash
docker pull ghcr.io/ttt599536561/sub2api-custom:0.2.17-r6
```

固定内容地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:e19bb55853094c3da1df7649b071b8235bba4fcc2164ca7f87c7f463d51155f1
```

## 升级与回退

1. 先备份数据库、Compose、应用环境和配置，保留原密钥、数据卷及回退镜像。
2. 新增 `246_enable_gemini_image_generation_groups.sql` 会把已有Gemini/Antigravity分组的生图开关false改为true；Composite/OpenAI不变。升级前核对策略，需禁用时升级后在后台关闭，后续启动不会再次强制开启。
3. 两个246迁移按完整filename区分，305→306已验证。不要重命名旧SQL或改checksum。
4. 从r4及更早版本升级还涉及QQ的248–250迁移；首次正式启用QQ须遵循[r5升级准备与资金回退说明](CUSTOM_IMAGE_0.2.17-r5.md)，保留独立加密主密钥，避免正式/测试实例同时处理同一QQ收费指令。不要恢复旧数据库覆盖新订单和余额。
5. 使用现有Compose参数仅更新应用镜像，保留PG/Redis及代理配置；检查health、迁移、版本、余额和已启用模块。镜像发布不等于部署：本任务未操作正式站、QQ测试站、Cloudflare或检测站，也未调用收费模型/发送QQ。

## 本机交接与清理

原始证据在 `output/publish-v0.2.17-r6` 和 `.cache/merge-ac1b8f1-20261026`，关键身份已归档本页。源码和控制提交已合入主目录并推送GitHub；原11份WIP在所有后续整合中指纹未变。

本轮Docker磁盘实际路径已核对在E盘，测试容器自动清理；引擎已恢复最初停止状态并保留共享镜像缓存。本轮native PG已停止、端口释放。其临时pgdata删除被自动审批以 `blocked by policy` 拒绝，未提供具体原因；目录保留，没有通过换工具或删除父目录绕过。没有清理旧工作树、用户资料或登录信息。

本轮已完全合入的隔离工作树从Git注销后，Windows删除目录返回Directory not empty；只针对该已核对残留目录的本机清理亦在启动前被自动审批以blocked by policy拒绝（无具体原因），未执行、未再次尝试或绕过。残留目录保留，完整源码仍在main和保留的已合并分支，原WIP保护快照不受影响。
