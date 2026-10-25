# Sub2API 二开 0.2.17-r5 发布与升级记录

日期：2026-10-25（Asia/Shanghai）。**镜像已发布并完成双架构与匿名完整运行层核验。** 用户授权只构建 Linux 镜像；不运行 macOS，不部署生产。固定身份及实际验证见下文。

## 固定来源

- 功能与审查提交：`4efa5ae4e410f603c60dd84f8a15555e16aa1984`。
- 最终应用源码：`1dd5a33a3d77e2394be906bdb5f9375bd0cb4d1d`，包含后续依赖安全与Go错误检查修正。
- 已发布镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.17-r5`，只包含 `linux/amd64`、`linux/arm64` 两个运行平台。
- [专用工作流](../.github/workflows/publish-custom-v0217-r5.yml)固定上述应用SHA，先要求该SHA最新main/push CI与Security Scan成功，再分别在原生Linux runner构建并运行检查；发布前后均禁止覆盖已有r5。
- 主目录原Codex/CCSwitch等WIP保留，不属于该固定来源；GitHub镜像只使用提交快照。

## 本次变化

完整QQ群生图：邮箱补验与双端授权、群识别/白名单、官方WebSocket/HTTP接收、余额原子预留和结算、历史订单/私有领取/QQ投递、后台配置与回复模板。另包含此前已在本地主线提交的监控刷新稳定性修复。

发布前修复反代Origin、页签草稿/保存生命周期、恢复/派发批次饥饿、私有图暂存期限及provider Key撤销误阻历史投递；支持紧邻生图提示词并与独立QQ菜单Worker分流一致。安全升级仅x/image0.45.0、DOMPurify3.4.17，Go仍1.27.3，无新增豁免。

详见[全面审查](../docs/changes/2026-10-25-qq-release-review.md)及[安全/质量门禁修正](../docs/changes/2026-10-25-r5-security-and-lint.md)。本机完整unit最终59包、前端442文件3957项通过；后续安全/lint补丁的实际专项验证单独记录，不与早期全量数字混为同一次运行。最终精确源码的Linux CI与Security Scan现均通过。

## r4 升级前的准备

1. 备份正式数据库、Compose、`.env` 与应用配置，记录当前r4镜像/digest。升级只替换应用镜像，保留数据库和Redis卷，不用测试站数据覆盖生产。
2. 本功能新增迁移248、249、250；应用启动按正常迁移流程执行。SQL历史未改写；已有QQ订单和冻结未排空前，不应退回不识别QQ资金记录的旧程序。
3. QQ功能默认关闭。首次启用前为正式实例设置独立的 `QQ_IMAGE_ENCRYPTION_KEY`（32字节随机值的64位十六进制，例如在服务器用 `openssl rand -hex 32` 生成）；安全备份并在后续升级保持不变，不能随意轮换，否则历史QQ/上游凭据无法解密。不要把实际密钥提交Git或写进公开文档。
4. 使用现有管理员设置完成正式站地址、SMTP、真实邮箱、机器人/上游接口与群识别；明确设置规范 `frontend_url`，或在HTTPS反代下显式配置正确 `server.trusted_proxies`。不要信任任意来源的转发头。
5. 单独使用本应用时保留默认 `QQ_IMAGE_EXTERNAL_HELP=false`。与现有智商QQ服务共用机器人时，须协调菜单服务版本/接收方式/文案来源，再启用外部帮助；`QQ_REPLY_TEMPLATE_SYNC_TOKEN` 只在需要该独立同步时配置。Cloudflare Worker不是此Docker镜像的一部分，不会因换镜像自动更新。
6. 正式站接管同一QQ App/群前，先停用测试站对应接收器，或改用独立机器人；不要让两个独立Sub2实例同时创建同一指令的生图订单。测试账号、测试余额及绑定不自动迁移到生产用户。

## 用户执行升级时

将现有Compose应用服务的image改为上述r5标签或最终固定digest，再使用现有 `-f` / project 参数执行应用服务pull与up。保持原端口、网络、卷、PG/Redis及代理配置。检查启动迁移、health、版本、原用户/余额及QQ连接；只在明确配置后由用户发起真实生图，真实上游可能收费。

如需回退，先停新申请、处理在途/未知订单和冻结资金，再评估旧应用兼容性；不要直接删QQ表、恢复旧数据库覆盖新数据或假定旧版本能处理新hold。保留回退镜像、迁移后的备份与加密主密钥。

## 已完成的发布验证

| 项目 | 结果 |
| --- | --- |
| 应用源码 / OCI revision | `1dd5a33a3d77e2394be906bdb5f9375bd0cb4d1d` |
| 发布控制提交 | `d3189e6c9cb79011d403497fd06b20c88e536081` |
| 最终index digest | `sha256:482c9f9a891ffaea15a745bae08706ad044f649850efd2380e1a7d08cf0186dd` |
| 精确源码CI | [40183525401](https://github.com/Ttt599536561/sub2api/actions/runs/40183525401)，所有5个job成功，包含unit/integration及QQ真实PG。 |
| 精确源码安全扫描 | [40183525405](https://github.com/Ttt599536561/sub2api/actions/runs/40183525405)，后端与前端门禁成功。 |
| 双架构发布 | [40184511319](https://github.com/Ttt599536561/sub2api/actions/runs/40184511319)，gate、amd64、arm64、publish共4job成功。 |
| 两架构实际二进制 | 版本r5、源码SHA、Go1.27.3及x/image0.45.0匹配；pg_dump/psql/价格资源、setup状态及嵌入前端检查通过。 |
| 工作流产物 | 5份ZIP的SHA256与GitHub元数据一致，run/control/source绑定通过。 |
| 匿名registry | index/runtime manifest/config/platform/revision/version及artifact绑定通过；全部24个运行层、90,132,419字节SHA256/大小通过，末尾重读标签仍一致。 |

核验时间：北京时间 **2026-10-25 18:33:14**。没有运行macOS，未向本机Docker引擎拉取镜像；运行层通过HTTP流式读取校验。未验证attestation内容或签名。安全门禁保留既有xlsx例外，未新增豁免；Go扫描仍提示2个未调用模块漏洞，不宣称全部依赖零漏洞。

| 平台 | Runtime manifest | OCI config |
| --- | --- | --- |
| `linux/amd64` | `sha256:6dbe1addf94cf7e0d512d730cd891ce4b950242611fe819fa9f15b7a929ef078` | `sha256:b2a207af9cdaf900611fe8294586aee55acf334132c04cc1b662e8accfa14bc5` |
| `linux/arm64` | `sha256:821914a1a4300efa8daeca4368206d775f800f396481591e43b8beceb0c78f8c` | `sha256:5be4165f5f89ef622088abe36ed8d1dec64e22110a55fa43791a978d6f8f3853` |

可直接拉取：

```bash
docker pull ghcr.io/ttt599536561/sub2api-custom:0.2.17-r5
```

固定内容地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:482c9f9a891ffaea15a745bae08706ad044f649850efd2380e1a7d08cf0186dd
```

证据在主项目 `output/publish-v0.2.17-r5` 与 `.cache/qq-release-review-20261025`；关键身份已归档本页。源代码已整合主目录并推送，原11份tracked WIP与非构建untracked内容保全，`.gitignore`/交接冲突保留双方信息；本机原Codex/版本同步157个PASS事件及CCSwitch25项通过，未把这些WIP打进镜像。

菜单服务另经用户授权仅轮换管理令牌后恢复在线，用户确认收到菜单；没有改BotSecret或盲改重连源码，旧停滞内部原因仍未证实。该独立CF服务不是本镜像的一部分。

本轮native测试PG已停止、端口释放；删除其临时pgdata、两份旧复现场景及两份发布辅助临时夹具被自动审批以blocked by policy拒绝，原位保留未重试。原工作树、WIP保护引用、登录资料和依赖缓存保留。**本轮没有部署或迁移生产服务器。**
