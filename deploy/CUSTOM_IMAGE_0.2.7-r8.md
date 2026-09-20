# Sub2API 0.2.7-r8：退款金额精度修复

2026-09-21 已发布，支持 `linux/amd64`、`linux/arm64`。

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.7-r8
```

固定 digest：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:6c83d34fe7f7c244ece15da0c01e7d3cb70bd9f4b227bf6957c5429de3f03be9
```

- 应用源码：`a718ad29b44812411bfdba764579006479ea1724`。
- 发布配置：`ac8c7dcddf4b698a2e528c8df434ea5e4da13086`。
- [完整源码 CI（成功）](https://github.com/Ttt599536561/sub2api/actions/runs/35522074513)：包含全部后端 unit/integration、Go lint、前端类型与关键测试、部署脚本检查。
- [源码安全扫描（成功）](https://github.com/Ttt599536561/sub2api/actions/runs/35522074594)。
- [双架构镜像构建与发布（成功）](https://github.com/Ttt599536561/sub2api/actions/runs/35522880600)。

## 修复

管理退款接口在调用支付网关前拒绝超过两位小数的退款请求金额，返回 `INVALID_AMOUNT`。这避免网关按原始精度退款、数据库按两位小数保存后，套餐抽奖次数回退计算不一致；也避免极小退款落库为零而导致本地收尾持续失败。

网关已确认退款的重试仍使用已保存的退款金额完成本地收尾。修复包含 `5.001`、`0.001` 等边界回归，以及正常金额、不同网关币种和已确认退款恢复测试。

## 验证与升级

本地回归测试、完整 service 单元测试、Go lint（0 issues）及后端编译通过。上述源码 CI 完成了本地 Docker 不可用时未能重跑的真实数据库集成测试。

两个原生架构 runner 均验证镜像版本、源码 revision、PostgreSQL 客户端、资源目录、初始化接口和内嵌前端。发布后匿名读取最终清单成功，校验内容 digest，并逐项确认架构、媒体类型、大小和子清单 digest 与通过运行验证的构建产物一致。

```bash
docker pull ghcr.io/ttt599536561/sub2api-custom:0.2.7-r8
```

在原部署目录将应用服务的 `image:` 更新为本文镜像，沿用全部原 Compose 参数重建应用服务。完整操作框架见 [r5 升级说明](CUSTOM_IMAGE_0.2.7-r5.md)，其中版本、源码 SHA 和 digest 替换为本文值。

从 r7 升级无新增数据库迁移。若从更早版本升级，先查看 [r7 迁移说明](CUSTOM_IMAGE_0.2.7-r7.md)。本修复阻止新请求产生精度丢失，不自动改写历史退款数据。
