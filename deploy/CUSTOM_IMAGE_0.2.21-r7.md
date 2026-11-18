# 0.2.21-r7 Linux 镜像

2026-11-18 13:46（北京时间），双架构镜像已发布到 GitHub Container Registry。服务器部署由用户安排。

```sh
docker pull ghcr.io/ttt599536561/sub2api-custom:0.2.21-r7
```

固定镜像索引为 `sha256:6f1b62a84ada136a71452a2d830653f8edd6853297c1aa5e102974f134678838`。按架构读取的正式镜像清单如下。

| 平台 | 清单 SHA-256 |
| --- | --- |
| linux/amd64 | `ea72272a18c3c325e50ec790cf6558f34e8000adf34638c60febe441546da9ce` |
| linux/arm64 | `7cd397a046fcd2e663f5f55aa36bc749cc6d79f71e27800e6a0fe64ab98bcfd3` |

## 构建范围

应用源码为 `2a9189b343afb961f6c42eef635357ef05871bee`。发布流程在 GitHub 的 Linux amd64 和 arm64 原生运行器执行，已在两种架构上分别启动容器，验证版本、源码、运行资源、初始化接口和前端资源。[发布运行](https://github.com/Ttt599536561/sub2api/actions/runs/44245924698)

发布工作流提交为 `ba4e2005ddaaebbf3a8311287ad10ffd2f71b3c1`。应用和工作流提交均已通过完整 CI 与安全扫描。[应用 CI](https://github.com/Ttt599536561/sub2api/actions/runs/44243939396) · [应用安全扫描](https://github.com/Ttt599536561/sub2api/actions/runs/44243939642) · [工作流 CI](https://github.com/Ttt599536561/sub2api/actions/runs/44243983719) · [工作流安全扫描](https://github.com/Ttt599536561/sub2api/actions/runs/44243983794)

## 版本和升级

本版在有效普通月卡续费到账时顺延 30 天并立即加入一个月额度，保留已用量。月额度刷新按订单去重。QQ 入群邀请自动发码已退役，已有福利月卡和仍有效的老码继续可用。[实现与回归验证](../docs/changes/2026-11-18-paid-renewal-invite-retirement.md)

从 r6 升级会新增迁移 264，数据库迁移记录由 324 条变为 325 条。升级前备份数据库和现有部署配置，保留数据卷。本版新续费收据开始写入后，回退应考虑数据库和应用的兼容性。

## 发布前修正

Go 静态检查发现的退款查询清理和无调用方法已修正。真实 PostgreSQL 18 的测试发现老码删除存在 RESTRICT 错误码差异，修复后已使用的历史收据继续得到保护。两个退款夹具补齐履约时间，三个旧版本升级用例显式验证了新投影为 NULL。原失败用例已在 PostgreSQL 18 复现并通过复测，服务包完整集成测试及 GitHub 全量测试均已通过。

## 匿名拉取验收

北京时间 13:47，已独立从 GHCR 匿名读取正式标签、两种架构的 manifest 和 config。24 个运行层共 92,714,546 字节，大小和摘要均通过校验。最终重读标签得到相同索引摘要。另核对了五份必要发布产物、两种架构的 12 项打包检查和两份 Docker 构建记录。

一次本地预检的临时源码及二进制仍在 E 盘项目缓存 `.cache/publish-v0221-r7-20261118/local-package-marker-check-4c2560140`，递归清理被自动审批以 `blocked by policy` 拒绝。
