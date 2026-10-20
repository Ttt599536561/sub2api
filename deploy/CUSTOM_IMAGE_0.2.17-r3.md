# Sub2API 二开 0.2.17-r3 发布与升级记录

日期：2026-10-21（Asia/Shanghai）。按用户要求，固定发布源码已通过 GitHub Linux CI、安全扫描及原生双架构构建 / 运行检查，GHCR 镜像已发布并完成匿名全部运行层校验。本次没有构建或运行 macOS 任务；生产仍运行已核验的 0.2.17-r2，本轮不升级应用。上一版见 [0.2.17-r2](CUSTOM_IMAGE_0.2.17-r2.md)。

## 发布身份与状态

| 项目 | 记录 |
| --- | --- |
| 已发布镜像 | `ghcr.io/ttt599536561/sub2api-custom:0.2.17-r3` |
| 功能实现提交 | `14aec28502a0e26cf97895d8d265f7d978f3ac6f`，专属推广子域名功能 |
| 镜像源码 / OCI revision | `c1b2134d091c8f0dabe9715788043c2f05ce8bb8`，包含 Linux CI 范围调整的完整发布快照 |
| 工作流控制提交 | `4950ee5b6258aa5137f9b1c9fd10ead6702e1442`，将发布来源固定为上述快照；不改变运行时代码 |
| 专用工作流 | [publish-custom-v0217-r3.yml](../.github/workflows/publish-custom-v0217-r3.yml) |
| 精确源码 CI | [运行 39468514593](https://github.com/Ttt599536561/sub2api/actions/runs/39468514593)，main / push，success |
| 精确源码安全扫描 | [运行 39468514358](https://github.com/Ttt599536561/sub2api/actions/runs/39468514358)，main / push，success |
| 构建发布 | [运行 39470386469](https://github.com/Ttt599536561/sub2api/actions/runs/39470386469)，attempt 1，gate、amd64、arm64、publish 四个 job 全部 success |
| 多架构 index digest | `sha256:52736abda13f53c41a051c2d11a96ca07c8ed5d6c64f3f87731c2a12202e6337` |
| 架构 | `linux/amd64`、`linux/arm64`，分别由原生 runner 构建和运行检查 |
| 生产状态 | 保持 0.2.17-r2；本轮未替换应用容器、未执行迁移 247、未绑定推广账号 |

可按标签拉取，或使用以下固定内容地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:52736abda13f53c41a051c2d11a96ca07c8ed5d6c64f3f87731c2a12202e6337
```

发布来源和控制提交分开记录。工作流先核验精确源码的最新 main / push CI 与 Security Scan 均成功，再构建两个架构；每个架构运行检查完成后才合成最终索引。构建前及最终索引发布前均检查标签不存在，禁止覆盖已发布 r3 标签。

## 本次 CI 与平台范围

自动部署脚本检查运行在 Ubuntu 24.04，包含 `bash -n deploy/apple-container.sh` 及原有四项 Compose / 运行资源 / Caddy 检查。依赖 `plutil`、BSD `stat` 的 Apple 容器测试保留在 [apple-container-ci.yml](../.github/workflows/apple-container-ci.yml)，只有 `workflow_dispatch` 手动入口；本次未运行该工作流或 Apple 容器运行测试，也没有构建 macOS 产物。Linux arm64 镜像属于本次发布范围。

历史首轮功能基线 `14aec2850` 的 CI 因 macOS 托管 runner 容量限制而取消了未获分配的 shell job，整体未通过；这不是 Apple 脚本执行失败。之后将自动 CI 限定 Linux，提交并固定新快照 `c1b2134d0`，本轮发布所用检查全部属于该新快照，不沿用旧提交结论。

## GitHub 检查与镜像验证

以下统计均来自固定发布快照 `c1b2134d091c8f0dabe9715788043c2f05ce8bb8` 的本轮检查：

| 范围 | 实际结果 |
| --- | --- |
| 后端 unit | `go test -tags=unit ./...`，58 个测试包通过，0 个失败包。 |
| 后端 integration | `go test -tags=integration ./...`，52 个测试包通过，0 个失败包。 |
| 前端 | 类型检查通过；CI 关键套件 30 文件 / 581 项通过，不代表执行了全前端测试集。 |
| Go lint | golangci-lint 2.14.0，0 issues。 |
| 部署脚本 | Linux 上 Apple 脚本语法及四项 Compose / 运行资源 / Caddy 检查通过。 |
| 发布辅助 | 10 项测试及发布脚本语法检查通过。 |
| 后端安全 | govulncheck 门禁通过；未报告代码可达或已导入包漏洞，另提示 7 个未调用的依赖模块漏洞。 |
| 前端安全 | 审计例外验证及其 19 项校验器测试通过；日志未输出完整 audit JSON，不能据此宣称依赖零漏洞。 |

Go CI 为非详细输出，以上是包级汇总；无法从该日志推算单项 / 叶测试通过数或跳过数。源码实现阶段的本机验证另见[实施记录](../docs/changes/2026-10-20-affiliate-subdomain.md)，不与本轮 CI 数字混用。

两个原生架构均验证了程序版本 `0.2.17-r3`、完整源码提交、镜像平台和 OCI revision，以及 PostgreSQL 客户端、定价资源、`/setup/status` 初始安装状态和前端 `/assets/` 资源。安装状态隔离检查没有连接生产数据库，不代表迁移、真实 OAuth 或充值返利已经验收。

匿名校验完成于 **2026-10-21 02:03:43（Asia/Shanghai）**，对应 UTC `2026-10-20T18:03:43.796013Z`。最终 tag / index、两个 runtime manifest / config 的内容 SHA256、descriptor 大小、架构、revision / version，以及全部 **24 个去重运行层、89,516,749 字节**均已核验；工作流保存的架构 digest 与最终运行镜像绑定一致。校验没有写入本机 Docker 缓存，本次未验证 attestation 内容。

| 平台 | Runtime manifest digest | OCI config digest |
| --- | --- | --- |
| `linux/amd64` | `sha256:33d3a22a0ecc1ba4135774d3cd3ebfa52b4c6b9dedc95abdcc1c6fcb9abd45c1` | `sha256:c87b22c60a912d1c143962a153fc94705c389fd71f9037ba539443c4506dde2e` |
| `linux/arm64` | `sha256:72da7cd58cf84cd0a5d0d411f59bd23b68e65ca152af2650a8dd5dc8a91682bd` | `sha256:c5d0b694d1e6872cd1a254091eb5d2e741e4205afc07779388bc63fb6a046693` |

工作流保存并经运行检查验证的架构产物 digest：

- `amd64`：`sha256:1c2c81b7662eb543a7f5d56a863b59e065a21a7a66e73810ecaaf45e885bc224`。
- `arm64`：`sha256:b5c514501dba64c9dfdd02b444239cbdddbf0440c35dd3a943b1be568d863118`。

## 变化与升级边界

相对 r2，新增管理员配置专属推广子域名、一次性不透明票据与主站受保护注册归因。邮箱及第三方新用户注册按事务保存邀请关系；首次有效专属来源不会被后来的 URL / 请求体邀请码覆盖，已有账号登录不改绑。专属域名配置独立于邀请码和返利参数。详细实现见上述实施记录。

新增迁移 `247_affiliate_promotion_hosts.sql`，包含域名映射、版本与删除用户时的失效处理。未来升级需要一起部署前后端并成功应用该迁移，保留既有 PostgreSQL、Redis、应用数据、端口及环境配置；升级前备份数据库和持久配置。

启用专属入口还需配置 DNS、HTTPS、主站 HTTPS origin、保留 Host 的反向代理及实际推广账号；主站交接路径须禁用缓存并避免记录票据查询参数。完整步骤见[启用说明](AFFILIATE_SUBDOMAIN.md)。镜像发布不会自动保存域名归属，也不替代线上注册和返利验收。

域名停用、改绑或账号删除会使旧票据和未完成归因失效；映射墓碑及版本不得手工删除或重置。回退到不支持此功能的旧应用前应停止推广入口并处理在途注册，已完成的邀请关系继续保存在原有表中。

## 生产准备与证据

生产仍为已核验且健康的 r2，应用 Compose 哈希和运行中的应用容器保持原版本；迁移 247 和推广域名账号映射尚未执行，专属入口目前返回维护状态 503。数据库 / globals / 应用配置 / Nginx 备份已生成；数据库备份完成目录读取和全流解码检查，未执行实际恢复。专属证书、Certbot timer、实际签发时的部署 hook 与兼容续期 dry-run 均完成各自验证。

用户已取消服务器升级路线，远端临时覆盖层镜像、上传包、解包目录和未激活的 Compose 候选已清理，备份与证书配置保留。此前覆盖层未通过的隔离检查不作为本次正式镜像运行证据；正式 r3 镜像由仓库 Dockerfile 从固定源码完整构建。

本机证据位于主项目 `output/publish-v0.2.17-r3`，包括 `checks-summary.json`、`backend-check-summary.json`、Actions jobs / artifacts、`registry-verification.json` 与 digest 绑定；必要身份、结果和边界已归档在本页。实际域名、账号、服务器信息仅保存于私有交付清单。

先前临时 PostgreSQL 数据、旧工作树残余及本轮取消维护工具草稿的删除被自动审批拒绝，仍按[交接记录](../docs/HANDOFF.md)原位保留，未重试或绕过；这些本机文件不属于发布源码或镜像。生产升级仍是后续独立操作，本轮只交付已发布镜像地址。

维护入口：[当前交接](../docs/HANDOFF.md)、[更新日志](../CHANGELOG.md)。
