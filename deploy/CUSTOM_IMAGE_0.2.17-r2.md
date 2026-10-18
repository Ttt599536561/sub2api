# Sub2API 二开 0.2.17-r2 发布与升级记录

日期：2026-10-18（Asia/Shanghai）。审查修复已合入主目录并推送 GitHub main；精确源码的 CI、安全扫描、双架构构建与运行检查成功，GHCR 镜像已发布，匿名完整运行层校验通过。生产未部署。上一版见 [0.2.17-r1](CUSTOM_IMAGE_0.2.17-r1.md)。

## 发布身份与状态

| 项目 | 记录 |
| --- | --- |
| 镜像 | `ghcr.io/ttt599536561/sub2api-custom:0.2.17-r2`（已发布） |
| 镜像源码 / OCI revision | `19b66b0efa2fff5006d512693524c50ff151a0ae`，九类复审修复源码 |
| 修复交接记录 | `eeb58522cb77802151b66821baedcf887d4f07f5`，仅文档补记，不改变应用代码 |
| 专用工作流 | [publish-custom-v0217-r2.yml](../.github/workflows/publish-custom-v0217-r2.yml) |
| 工作流控制提交 | `ccd1a7694e2529cd096f68ea25998e9bf858a354`；仅 CI/发布记录变化 |
| 源码 CI | [运行 39134324150](https://github.com/Ttt599536561/sub2api/actions/runs/39134324150)，success |
| 源码安全扫描 | [运行 39134324148](https://github.com/Ttt599536561/sub2api/actions/runs/39134324148)，success |
| 构建发布 | [运行 39135294766](https://github.com/Ttt599536561/sub2api/actions/runs/39135294766)，attempt 1，四个 job 全部 success |
| 多架构 index digest | `sha256:e0cb7dca1f9fd9d8d1bb16fe4592f2a19bfb6587e22b2e7710c1fe13f3812076` |
| 架构 | `linux/amd64`、`linux/arm64` |
| 生产部署 | 未执行 |

先单独推送固定源码，使上述 CI / Security Scan 确认为该完整 SHA 的 `main` / `push` 成功运行；再推送独立工作流控制提交并 dispatch。两个架构在原生 runner 构建、验证后才发布最终索引。r1 和 r2 的最终标签均禁止覆盖；没有创建通用 v* tag 或发布其他镜像/发行资产。

可用于固定内容部署的地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:e0cb7dca1f9fd9d8d1bb16fe4592f2a19bfb6587e22b2e7710c1fe13f3812076
```

## 变化与升级边界

相对 r1，包含退款审计顺序/状态/持久化与支付宝未知结果恢复、Messages 实际模型准入、HTTP/WS 余额预占与冷却、渠道模型目录及支付页面路由归属，共九类修复。完整复现、本机全量验证和条件跳过见[审查报告](../docs/changes/2026-10-18-review-1cfafd30e.md)。

本版不新增 SQL/Ent schema、依赖或部署参数。由 r1 升级时替换应用 image 并保留数据库、Redis、端口、卷和环境配置；更早版本同时遵循 r1 的迁移 246 与认证快照 v25 说明。升级前备份数据库和持久数据，启动后核验实际版本及业务。持续数据库不可写或首次持久化前退出仍可能遗留需人工核对的 REFUNDING；不能重复调用网关代替对账。未知支付宝退款按原请求号只查单。

## 主目录与既有修改

主目录 `E:\sub2二次开发项目` 已切换到 `main`，从旧基线快进到修复、发布控制及后续记录；旧 `codex/workspace-v0.2.14` 分支保留。原有 Codex/CC Switch 和本机环境说明等 10 个 tracked WIP、5 类 untracked 内容保留，未纳入镜像源码。旧生成资源未用作镜像输入。

原 WIP 已保存 tracked stash、受保护本机引用、原补丁及 15 文件 SHA256 快照，位于主项目 `.cache/publish-v0.2.17-r2-20261018/main-wip-backup`。恢复后 7 个未重叠 tracked 文件与 5 个 untracked 文件逐字节相同；3 个共同修改文件的 WIP 补丁与原备份一致，新版改动也保留。未合并索引为空。主目录现有依赖上的 Codex 身份/同步专项 34 顶层、131 叶测试通过，CC Switch 2 文件、29 项通过，均 0 失败/跳过；测试后 WIP 哈希未变。以上针对本机 WIP 的兼容验证，不表示 WIP 已发布。

## GitHub 与镜像验证

本轮 CI 后端 unit 58 个包、integration 52 个包通过；前端类型/lint 与 30 文件、581 项关键测试通过，Go lint 0 issues，部署脚本和 10 项发布工具测试通过。前端 CI 是关键套件，Go 包数不是独立用例数；非详细日志不提供逐项跳过数量。本机完整验证另见审查报告。

安全扫描通过：govulncheck 未报告代码可达漏洞，提示 7 个未被代码调用的模块级漏洞；pnpm audit 按现有依赖例外校验通过，审计校验器 19 项测试通过。该结论不等同于依赖绝无漏洞。

两个架构运行检查均确认 `Sub2API 0.2.17-r2` 和完整源码 `19b66b0efa2fff5006d512693524c50ff151a0ae`，并验证 PostgreSQL 客户端、定价资源、安装状态及前端资源。安装页 smoke 没有连接生产数据库，不代表生产迁移和真实业务验收。

匿名校验时间：2026-10-18 22:25:14（Asia/Shanghai）。最终 tag/index、runtime manifest/config 的原始 SHA256、descriptor 大小、平台及 source/revision/version 标签均通过；全部 24 个去重运行层共 89,414,882 字节已流式下载并逐层核验大小和 SHA256，未写入本机 Docker 缓存。仅跳过明确标注的 attestation descriptor，未验证证明材料内容。

| 架构 | Runtime manifest digest | OCI config digest |
| --- | --- | --- |
| `linux/amd64` | `sha256:f14f2a399c1c915f49f1bb2ef7dae60316927d6936b6955a6ca1f15f2f8451c2` | `sha256:ede07ffcbeb9aa63a6fba6f04756565c3d767a28ac734bafa466bb7b3e4f84b6` |
| `linux/arm64` | `sha256:4e5b0045efaf225b431173b8f263d4f83798659c03fcceffa1ab147b5edb5fd5` | `sha256:756f3b0ed27e12b337c0a93adf9945e85d88bcc49bd34ab0de230fe85f9f07d6` |

经过运行检查并由本次 workflow artifact 绑定的架构产物 index：

- `amd64`：`sha256:5b0a5197012250e366017c3fbb7ba3ce02d2b22281e3215332c1116b846dff75`。
- `arm64`：`sha256:0ae9e25b24849c0e7a955af71ae4837d3bf8e51a677f0963107aa9fe8989549d`。

## 证据与清理

本机证据在主项目 `output/publish-v0.2.17-r2`：三个 Actions 运行身份、jobs/logs/artifacts、实际 digest 文件、匿名校验脚本及 JSON、主目录恢复与专项摘要、预检及哈希清单。关键身份和结论已在本页受 Git 跟踪保存；工作流控制与后续文档不改变固定应用源码。

本轮 6 个临时准备脚本（3,981 字节）的单次限定清理被自动审批以 `blocked by policy` 拒绝，未给出具体规则；文件原位保留，没有改工具或重试绕过。WIP 备份、共享依赖缓存及发布证据均保留。旧任务的清理拒绝文件未重试。

维护入口：[当前交接](../docs/HANDOFF.md)、[更新日志](../CHANGELOG.md)。镜像发布成功不代表生产已升级。
