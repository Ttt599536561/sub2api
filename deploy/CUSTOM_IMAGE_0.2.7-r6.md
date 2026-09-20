# Sub2API 0.2.7-r6 合并后复查镜像

2026-09-20 已发布，支持 `linux/amd64` 和 `linux/arm64`，无需登录即可公开拉取。

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.7-r6
```

固定不可漂移引用：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:092d5aa48b24db1e9bbc303cf37558c84107ea4fda2fa6f85bde2a71366d78e9
```

- [GitHub 镜像构建与发布记录（成功）](https://github.com/Ttt599536561/sub2api/actions/runs/35514385691)
- 发布配置提交：`bed632f735b336f8ddad74649331e1688ed1e5e9`。
- 固定应用源码：`6bd6654ee616a942410850be8902dc35111ec7bb`。
- [固定源码 CI（成功）](https://github.com/Ttt599536561/sub2api/actions/runs/35461419874) 与[安全扫描（成功）](https://github.com/Ttt599536561/sub2api/actions/runs/35461419868)。

## 审查结论与验证

重新审查上次合并 `1f428c024` 及之后的三个提交，未发现未解决的 Git 冲突或可证实的前后端合并回归。合并后只有发布配置和文档变化；本次无需业务代码修复。r6 固定构建本次复查提交，应用代码与 r5 相同，没有新增迁移。详细证据见[合并复查记录](../docs/UPSTREAM_MERGE_V0.2.7_2026-09-20.md)。

本次本地完整前端 2425 项、后端 20205 项测试/子用例，以及隔离数据库专项 87 项均通过；后端另有 16 项按环境条件跳过。前端 lint、类型检查、生产构建和内嵌前端的后端编译通过。

两个原生架构 runner 均按 digest 拉取并验证程序版本、源码 revision、PostgreSQL 客户端、资源目录、初始化状态接口和内嵌前端。发布后另行匿名读取最终清单，核实两个平台及附加清单与构建产物完全对应；本机使用空 Docker 凭据配置成功拉取最终 digest，运行输出 `0.2.7-r6`、上述完整源码 SHA，以及 PostgreSQL 18.6 客户端版本。

## 拉取与更新

```bash
docker pull ghcr.io/ttt599536561/sub2api-custom:0.2.7-r6
```

更新已有 Compose 应用时，沿用原部署目录和全部 Compose 参数，将最终生效的应用服务镜像设为：

```yaml
image: ghcr.io/ttt599536561/sub2api-custom:0.2.7-r6
```

备份、仅重建应用服务、核验实际镜像和健康状态的方法沿用 [r5 更新说明](CUSTOM_IMAGE_0.2.7-r5.md)，其中镜像版本、源码 SHA 和 digest 均替换为本文值。不要覆盖现有 `.env`、密钥、挂载或数据库配置；从 r5 升级没有新增迁移，从更早版本升级仍需按原升级说明处理福利迁移。

本次交付完成 GitHub 推送和镜像发布，未连接或部署生产服务器。
