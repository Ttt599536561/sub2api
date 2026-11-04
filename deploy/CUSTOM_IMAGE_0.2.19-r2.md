# Sub2API 二开 0.2.19-r2 Linux 镜像

## 发布状态

2026-11-04（北京时间）准备发布。目标标签为 `ghcr.io/ttt599536561/sub2api-custom:0.2.19-r2`。已核验的上一版见 [0.2.19-r1 发布记录](CUSTOM_IMAGE_0.2.19-r1.md)。r2 的运行编号及最终 digest 待发布后记录。

| 项目 | 准备状态 |
| --- | --- |
| 应用源码 / OCI revision | `1e3ca55416d9117258fc906bd1891bc0c64c8644`，包含全文本模型 Ultrafast 和预占修复。 |
| 精确源码门禁 | 专用工作流要求上述 SHA 在 main 的 push 事件对应的最新 CI 和 Security 均已成功。 |
| 构建及运行检查 | 原生 Ubuntu 分别构建 linux/amd64、linux/arm64，逐架构拉取 digest，核对平台、版本、revision、Go 版本，并运行检查 PostgreSQL 工具、价格资源、安装状态和嵌入前端。 |
| 最终标签 | 仅合并本次通过冒烟的两个架构 digest。构建前与写入最终标签前都检查标签是否存在，已有标签或查询异常时停止。 |

[专用工作流](../.github/workflows/publish-custom-v0219-r2.yml) 仅支持在本仓 main 手动触发。先推送应用 SHA 使其取得 main/push 门禁，再推送专用工作流；满足门禁后运行 `gh workflow run publish-custom-v0219-r2.yml --repo Ttt599536561/sub2api --ref main`。全程使用分支提交，不创建发行 Git 标签。

## 功能与升级

- 已开启强制 Ultrafast 的 OpenAI 分组将所有文本/推理模型按有效 Standard 价 ×8，覆盖自定义别名、模型映射及文本按次收费。媒体按张/按次和工具附加费保持原口径。详见[功能说明](../docs/changes/2026-11-03-ultrafast-all-models.md)。
- 通过 Responses 调用纯生图且可确认按张或按次时，按实际计价模型与图片倍率预占。配置为 Token 计费时沿用原规则。强制路由到 Antigravity 时按实际平台预占。详见[修复说明](../docs/changes/2026-11-04-ultrafast-reservation-fixes.md)。
- 从 r1 升级没有新增数据库迁移、依赖或配置字段。升级前备份配置和数据库，保留既有 QQ 加密密钥及接收器设置。协调切换所有节点，避免同一分组使用两套定价；回退到 r1 会恢复旧模型名单、上游降档和旧预占行为。
- 本次交付为 GitHub 上的 Linux 镜像，服务器替换由用户执行。当前服务器版本以实际运行信息为准。
