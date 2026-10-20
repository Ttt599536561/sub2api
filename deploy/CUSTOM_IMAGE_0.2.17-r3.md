# Sub2API 二开 0.2.17-r3 发布与升级记录

日期：2026-10-21（Asia/Shanghai）。用户要求通过 GitHub Actions 构建并发布镜像地址，本轮不升级生产应用。应用功能基线已推送 GitHub main；用户明确本次只运行 Linux 检查，正在调整自动 CI 范围并准备新的固定发布快照，r3 镜像尚未发布。上一版见 [0.2.17-r2](CUSTOM_IMAGE_0.2.17-r2.md)。

## 发布身份与状态

| 项目 | 记录 |
| --- | --- |
| 候选镜像地址 | `ghcr.io/ttt599536561/sub2api-custom:0.2.17-r3`（计划发布，尚未生成最终镜像） |
| 应用功能基线 | `14aec28502a0e26cf97895d8d265f7d978f3ac6f`，专属推广子域名功能源码 |
| 最终镜像源码 / OCI revision | 待本次 Linux CI 调整提交后固定新的完整发布快照；当前不预填 SHA |
| 功能实施与验证 | [专属推广子域名记录](../docs/changes/2026-10-20-affiliate-subdomain.md) |
| 专用工作流 | [publish-custom-v0217-r3.yml](../.github/workflows/publish-custom-v0217-r3.yml) |
| 初版工作流控制提交 | `521909fbe8daeadf505ba8a32495c332e2ae8f1e`；后续固定新发布快照的控制提交尚未产生 |
| 首轮功能基线检查 | [CI 39464790379](https://github.com/Ttt599536561/sub2api/actions/runs/39464790379) 的 Linux 检查成功，macOS 任务因托管 runner 容量排队而未执行，整体 CI 未通过；[Security Scan 39464790324](https://github.com/Ttt599536561/sub2api/actions/runs/39464790324) 成功 |
| 新发布快照检查 | CI 范围调整尚未提交；提交后需重新取得该完整 SHA 的 main / push Linux CI 与 Security Scan 成功记录 |
| 构建发布 | 尚未触发专用发布工作流 |
| 多架构 index digest | 尚未生成；发布成功后记录实际值，不预填或沿用旧 digest |
| 目标架构 | `linux/amd64`、`linux/arm64`，分别由原生 runner 构建和运行检查 |
| 生产状态 | 当前仍运行已核验的 0.2.17-r2；本轮不替换应用容器 |

版本标签已检查为尚不存在。工作流分别在构建前和发布最终索引前拒绝覆盖已有最终标签。发布固定应用源码与工作流控制提交分开记录：只有指定源码的最新 main / push CI 与 Security Scan 均完成且成功，才允许构建。r3 的 digest、运行链接和镜像验证须在实际完成后补记；当前不能据此宣称 r3 已可拉取。

## 本次 CI 与平台范围

自动部署脚本检查运行在 Ubuntu 24.04，包含 `bash -n deploy/apple-container.sh` 及原有四项 Compose / 运行资源 / Caddy 检查。依赖 `plutil`、BSD `stat` 的 Apple 容器运行测试移至 [apple-container-ci.yml](../.github/workflows/apple-container-ci.yml)，只有 `workflow_dispatch` 手动入口，没有 push / pull_request 触发，本次不执行，也不属于发布门禁。其余后端、前端、lint、发布辅助和安全检查保持不变。

本次仍发布 `linux/amd64` 与 `linux/arm64`；Apple / macOS 任务与 Linux arm64 镜像是不同范围。新发布快照提交并通过完整 Linux CI / 安全扫描后，再更新专用工作流的 `SOURCE_COMMIT`。当前工作流尚未重固定，不触发发布；运行时代码与已验证功能基线保持一致。

本机验证：YAML 解析与 13 段工作流 Bash 语法检查通过，Apple 脚本仅执行 `bash -n`；四项 Linux / Compose / Caddy 脚本检查全部通过，67 份运行时代码哈希与既有验证快照一致。本机没有运行 Apple 容器测试、macOS 任务或 Docker 引擎；新发布快照的完整 GitHub Linux CI 与安全扫描仍需在提交后执行。

## 变化与升级边界

相对 r2，新增管理员配置专属推广子域名、一次性不透明票据与主站受保护注册归因。邮箱及第三方新用户注册按事务保存邀请关系；首次有效专属来源不会被后来的 URL / 请求体邀请码覆盖，已有账号登录不改绑。专属域名配置独立于邀请码和返利参数。实现与测试范围见上述功能记录。

新增迁移 `247_affiliate_promotion_hosts.sql`，包含域名映射、版本与删除用户时的失效处理。未来升级需要一起部署前后端并成功应用该迁移，保留既有 PostgreSQL、Redis、应用数据、端口及环境配置；升级前备份数据库和持久配置。

启用专属入口还需配置 DNS、HTTPS、主站 HTTPS origin、保留 Host 的反向代理及实际推广账号；主站交接路径须禁用缓存并避免记录票据查询参数。完整步骤见[专属推广子域名启用说明](AFFILIATE_SUBDOMAIN.md)。发布镜像本身不会自动保存域名归属，也不会替代线上注册和返利验收。

域名停用、改绑或账号删除会使旧票据和未完成归因失效；映射墓碑及版本不得手工删除或重置。回退到不支持此功能的旧应用前应停止推广入口并处理在途注册，已完成的邀请关系继续保存在原有表中。

## 验证范围与发布检查

功能源码此前完成的本机单元、PostgreSQL、前端及静态验证，详见功能实施记录；这些历史结果不替代本轮 GitHub 检查。发布工作流将针对固定源码执行以下检查：

- 精确 SHA 的 main / push CI 与 Security Scan 门禁。
- 每个原生架构的源码身份、镜像平台和 OCI revision 校验。
- 镜像中程序的版本 / 完整提交、PostgreSQL 客户端及定价资源检查。
- 隔离容器的 `/setup/status` 初始安装状态及前端 `/assets/` 资源检查。
- 两个已验证架构 digest 合成最终索引，并核对索引同时包含 amd64 与 arm64。

安装状态检查不连接生产数据库，不代表迁移、真实 OAuth 或充值返利已经验收。此前本地覆盖层方案已按用户新要求停止，不能把其未通过的运行检查记为本次正式镜像验证；正式镜像从固定源码通过仓库 Dockerfile 完整构建。

## 生产准备与后续记录

主代理已核验生产仍为 r2 的既有 digest，数据库 / 配置 / Nginx 备份已生成；数据库备份完成目录读取和全流解码检查，未执行实际恢复。推广域名 HTTPS 入口当前提供维护响应，主站健康检查正常；Certbot timer 已启用，实际签发时的部署 hook 与兼容续期 dry-run 均已通过各自验证。应用 Compose 哈希与运行中的应用容器保持原版本，迁移 247 和域名账号映射尚未执行。已取消路线的远端覆盖层镜像及临时上传 / 解包 / Compose 候选文件已清理，备份和证书配置保留。实际域名、账号、服务器及本机证据路径仅保存于私有交付清单。

发布完成后补记：控制提交、CI / Security Scan / 构建运行链接、最终 index 与各架构 digest、实际校验范围。生产升级仍属于后续独立操作，不因镜像发布自动执行。

维护入口：[当前交接](../docs/HANDOFF.md)、[更新日志](../CHANGELOG.md)。
