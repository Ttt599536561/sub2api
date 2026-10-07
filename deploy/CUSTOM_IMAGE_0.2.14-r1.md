# 已发布：Sub2API 二开 0.2.14-r1

2026-10-07（Asia/Shanghai）已在 GitHub 完成构建与发布。此镜像包含官方 0.2.9～0.2.14 累积更新、原 v0.2.8-r1 的二开能力，以及两轮追加审查的修复。

## 镜像地址

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.14-r1
```

固定内容地址（部署需完全锁定内容时使用）：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:3a11e36016484c1ac6cce93ec85b5964c7439c32001305be569e94e33bc36f63
```

支持 linux/amd64 与 linux/arm64。两种架构均在原生 GitHub runner 构建和启动验证，并通过匿名 GHCR 索引、manifest/config 内容及 SHA-256 校验；服务器可直接拉取。

## 源码与 GitHub 验证

- 镜像源码/revision：`5874208c9c0d12418fedab0ee59e2650dd83808f`。
- 应用审查修复源码：`9e6da49e32aec8c91da1e611c3b6a6f5f20477be`。之后只添加发布文档、CI/发布流程，以及把 Dockerfile 的 pnpm 固定为已验证的 9.15.9；backend/frontend 应用源码没有变化。
- 整合分支：`codex/merge-upstream-v0.2.14`；发布分支：`codex/publish-v0.2.14-r1`（固定在镜像源码提交）。
- [发布前同 SHA 的 CI：成功](https://github.com/Ttt599536561/sub2api/actions/runs/37609509824)
- [发布前安全扫描：成功](https://github.com/Ttt599536561/sub2api/actions/runs/37609510423)
- [双架构构建与发布：成功](https://github.com/Ttt599536561/sub2api/actions/runs/37610808596)
- [发布分支 CI：成功](https://github.com/Ttt599536561/sub2api/actions/runs/37610808393)
- [发布分支安全扫描：成功](https://github.com/Ttt599536561/sub2api/actions/runs/37610808398)

每个架构先验证版本、源码标签、架构、pg_dump/psql、定价资源文件、容器启动、/setup/status 以及内嵌前端，再按已验证 digest 合成最终标签。最终清单精确包含两种 Linux 运行架构；额外构建证明条目不作为运行架构计算。架构中间标签带 run_id/run_attempt，最终版本拒绝覆盖。

| 平台 | CI 验证的架构索引 digest | 最终运行镜像 digest |
| --- | --- | --- |
| linux/amd64 | `sha256:efbe6c08fd24053ca406bf7d2128ca2e12ae3806f8b562bdd4f7783684aeaec1` | `sha256:4fb9bcf0e8471776bd201d486b3afcddccaff0be5e3f9e12d8dcead2f29ad2e5` |
| linux/arm64 | `sha256:5a862d53d111687380f92e15b38cbaec23abc62765871e2af89a602b53bbe4b8` | `sha256:bcb9110a9e288b7085cee160439f8bfbdc37d7815d32587951a9efa379bdb032` |

最终索引 digest：`sha256:3a11e36016484c1ac6cce93ec85b5964c7439c32001305be569e94e33bc36f63`。上表运行镜像逐一匹配 GitHub 上传的已验证架构产物，镜像 config 的版本、源码 SHA、源仓库也已独立核对。

首次 CI 的集成测试曾因新 runner 未准备好 postgres:18.1-alpine3.23 而失败，尚未运行数据库测试。镜像标签实际存在；增加固定 PostgreSQL/Redis/Ryuk 镜像的显式预拉取、最多三次重试和存在性检查后，完整 CI 已通过。没有修改业务实现、改数据库版本、禁用容器回收或跳过集成测试。首次失败不计作通过证据。

## 更新线上部署

先备份数据库，并保存当前 Compose/.env。保留数据库、Redis、配置、挂载与原环境变量，只修改现有应用服务的 image：

```yaml
services:
  sub2api:
    image: ghcr.io/ttt599536561/sub2api-custom:0.2.14-r1
```

在服务器现有 Compose 项目目录执行：

```sh
docker compose pull sub2api
docker compose up -d --no-deps sub2api
docker compose logs --tail=100 sub2api
```

服务名不同则替换 sub2api。需要固定内容时，将 image 的标签地址替换为上面的完整 digest 地址。不要执行 down -v，不删除数据库卷。

从 v0.2.8-r1 升级会执行充值赠金与 TypeSafe 两份新增迁移；历史 SQL 原样保留，三条历史升级路径已通过真实 PostgreSQL/Redis 验证。生产数据仍应单独备份；迁移后回退旧镜像不能替代恢复数据库备份。本任务没有连接或更新生产服务器，线上部署由用户执行。

本地发布证据位于 `E:/sub2二次开发项目/.cache/publish-v0.2.14-r1`，包括工作流检查、GitHub 运行状态、架构 digest 产物、原始 OCI JSON 和 verification.json。主目录原有未提交修改保持不变。
