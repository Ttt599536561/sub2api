# Sub2API 二开 0.2.19-r2 Linux 镜像

2026-11-04 已发布到 GHCR。服务器可在 Compose 的 `image` 字段使用下面的标签。

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.19-r2
```

固定内容地址为 `ghcr.io/ttt599536561/sub2api-custom@sha256:b129d2d7834fbc363d1b2f20cfd3596ad95d6190f5ba7b8cc1b0ee4215f64504`。

## 来源与验证

应用提交为 `1e3ca55416d9117258fc906bd1891bc0c64c8644`，发布工作流提交为 `e72ee5fd7f085b780d6126df2af30170a2a9a116`。本地主目录和 GitHub main 已整合这两个提交。服务器由用户自行切换镜像。[发布工作流](../.github/workflows/publish-custom-v0219-r2.yml)

| 检查 | 结果 |
| --- | --- |
| 应用源码的精确门禁 | [CI 41793518296](https://github.com/Ttt599536561/sub2api/actions/runs/41793518296) 和 [Security 41793518293](https://github.com/Ttt599536561/sub2api/actions/runs/41793518293) 通过，覆盖后端 unit/integration、完整前端、lint 和安全扫描。 |
| 发布控制 | [CI 41794002561](https://github.com/Ttt599536561/sub2api/actions/runs/41794002561) 和 [Security 41794002559](https://github.com/Ttt599536561/sub2api/actions/runs/41794002559) 通过。 |
| Linux 镜像 | [发布 41795291208](https://github.com/Ttt599536561/sub2api/actions/runs/41795291208) 成功。两个原生 Ubuntu runner 分别构建、拉取并实际运行镜像，检查版本、源码 SHA、PG 工具、价格资源、安装状态和嵌入前端。 |
| GHCR 匿名校验 | 北京时间 2026-11-04 15:59:34，已核对最终 index、运行 manifest、OCI 配置及 24 个运行层，共 90,618,449 字节。5 份 Actions 验证产物的摘要与大小一致。 |

| 平台 | Runtime manifest |
| --- | --- |
| linux/amd64 | `sha256:9f6d5d7f3cbeba9e029278d308ad8f6c498ea250638b7e9670cc5b3e6669a235` |
| linux/arm64 | `sha256:0bdb40df2bb46a904896f6cac7e8da26afdf6346620305c1e83bf432b2eec5df` |

## 功能与升级

- 已开启强制 Ultrafast 的 OpenAI 分组将所有文本/推理模型按有效 Standard 价 ×8，覆盖自定义别名、模型映射及文本按次收费。媒体按张/按次和工具附加费保持原口径。详见[功能说明](../docs/changes/2026-11-03-ultrafast-all-models.md)。
- 通过 Responses 调用纯生图且可确认按张或按次时，按实际计价模型与图片倍率预占。配置为 Token 计费时沿用原规则。强制路由到 Antigravity 时按实际平台预占。详见[修复说明](../docs/changes/2026-11-04-ultrafast-reservation-fixes.md)。
- 从 r1 升级没有新增数据库迁移、依赖或配置字段。升级前备份配置和数据库，保留既有 QQ 加密密钥及接收器设置。协调切换所有节点，避免同一分组使用两套定价；回退到 r1 会恢复旧模型名单、上游降档和旧预占行为。
- 本次交付为 GitHub 上的 Linux 镜像，服务器替换由用户执行。本轮交付范围为镜像发布。
