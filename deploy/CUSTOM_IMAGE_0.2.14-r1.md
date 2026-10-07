# 二开 0.2.14-r1 镜像发布与升级

本次发布包含官方 Sub2API v0.2.14、线上 v0.2.8-r1 的二开能力以及两轮追加审查修复。应用源码提交为 `9e6da49e32aec8c91da1e611c3b6a6f5f20477be`；发布流程提交新增版本专用工作流和本文，并将根 Dockerfile 的 pnpm 固定为本次已验证的 9.15.9；不改变应用业务源码。镜像 revision 以 GitHub 发布事件的完整提交 SHA 为准。

## 发布目标

- 仓库：Ttt599536561/sub2api。
- 整合分支：codex/merge-upstream-v0.2.14。
- 发布分支：codex/publish-v0.2.14-r1。
- 工作流：.github/workflows/publish-custom-v0214.yml。
- 镜像：ghcr.io/ttt599536561/sub2api-custom:0.2.14-r1。
- 架构：linux/amd64 和 linux/arm64，分别在原生 GitHub runner 构建与验证。

这份文件记录发布流程。镜像是否已经成功发布、实际 digest 和 GitHub 运行链接，以后续发布验证记录为准，不把目标地址当作已经可拉取的证明。

## 验证顺序

先推送整合分支，等待相同 SHA 的 GitHub CI 与安全扫描通过，再推送专用发布分支。工作流从事件的不可变 SHA 检出，校验基础版本文件为 0.2.14，注入二开版本 0.2.14-r1 和该 SHA。每个架构先验证镜像 revision、架构、程序版本、PostgreSQL 工具、运行资源、setup 状态及内嵌前端，再使用已验证 digest 合成最终标签。若最终标签已存在则失败，不覆盖已有版本；架构中间标签加入 run_id/run_attempt，最终清单结构化校验两种运行架构并输出 digest。手动触发也限定到专用发布分支。

## 线上更新

在现有 Compose 项目目录操作，先保存当前 Compose/.env 和数据库备份。将 sub2api 服务的 image 设置为发布验证记录中的固定标签或完整 digest；保留现有数据库、Redis、配置、挂载和环境变量。

```yaml
services:
  sub2api:
    image: ghcr.io/ttt599536561/sub2api-custom:0.2.14-r1
```

确认镜像已经发布后，只更新应用服务：

```sh
docker compose pull sub2api
docker compose up -d --no-deps sub2api
docker compose logs --tail=100 sub2api
```

如果实际服务名不是 sub2api，请使用现有 Compose 中的服务名。不要执行 down -v，不删除数据库卷。数据迁移后回退旧镜像是否兼容不能仅凭启动成功判断，保留升级前数据库备份。

从 v0.2.8-r1 升级会包含两个新增数据库迁移（充值赠金和 TypeSafe）；所有历史 SQL 保持原样，三条历史升级路径已在本地真实 PostgreSQL/Redis 验证。本轮不连接生产服务器，线上部署由用户执行。
