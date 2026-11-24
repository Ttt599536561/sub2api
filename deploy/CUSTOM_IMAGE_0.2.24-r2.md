# 0.2.24-r2 Linux 镜像

GitHub Container Registry 已发布此版本。2026-11-24 13:57（北京时间）完成匿名拉取验证。生产服务器由用户自行升级。

```sh
docker pull ghcr.io/ttt599536561/sub2api-custom:0.2.24-r2
```

固定镜像索引为 `sha256:46ddf5e5f84c409ea1bfbcf73bc784c8e7019a1ce44147ad23eaa3824cdf633b`。

| 平台 | 镜像清单摘要 |
| --- | --- |
| linux/amd64 | `sha256:4a12466cc92a435776d153cf342b1b74d69c5daa262ff905d234eeb4356caef6` |
| linux/arm64 | `sha256:cd2831cffaa55c93543f78c84d97488b7cf458743ba4511f1e6f02a17a2865bf` |

## 构建与验证

源码 `279c4ede399600c1bb81852f69a136a0a7074a17` 和发布工作流 `a6274406257985534f9c07b283de8515d3c6700d` 均已通过 GitHub CI 和安全扫描。[源码 CI](https://github.com/Ttt599536561/sub2api/actions/runs/45334065563) · [源码安全扫描](https://github.com/Ttt599536561/sub2api/actions/runs/45334065412) · [工作流 CI](https://github.com/Ttt599536561/sub2api/actions/runs/45334285896) · [工作流安全扫描](https://github.com/Ttt599536561/sub2api/actions/runs/45334285821)

GitHub 在两种 Linux 架构的原生运行器上分别构建并启动容器，核对版本、内嵌资源、启动页和初始化接口。两种架构的应用二进制均使用已修复的 Gorilla WebSocket 版本 `v1.5.4-0.20240701034025-d67f41855da4`。[镜像构建](https://github.com/Ttt599536561/sub2api/actions/runs/45336254321)

匿名验收核对了正式标签、两份架构清单、OCI 源码版本和 24 个运行层，共 93,100,780 字节。最终重读标签得到同一索引摘要。

## 从 0.2.21-r7 升级

- 升级前备份 PostgreSQL 和部署配置，保留数据卷与原有加密密钥。升级后迁移记录应从 325 项增至 329 项。
- Sub2 QQ 收件改用签名 HTTP 回调。原先使用 WebSocket 的实例，需在腾讯 QQ 开放平台设置 `https://<Sub2 公网域名>/api/v1/qq-image/events` 并订阅群消息事件。已经使用回调的实例沿用原地址。
- 网站绑定、订单、余额与历史作品沿用现有数据库。详细计费修复、迁移内容和回归结果见[本版变更](../docs/changes/2026-11-24-v0224-r2-review-fixes.md)。
