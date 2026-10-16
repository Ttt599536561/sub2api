# OpenAI 分组强制 Ultrafast：实施与验证记录

- **日期**：2026-10-17（Asia/Shanghai）
- **功能分支**：`codex/openai-group-ultrafast-20261017`
- **设计基线**：`b9c36c6ee`
- **当前状态**：已完成源码实现与本地验证，未发布镜像、未部署生产

## 已实现行为

- 管理员 OpenAI 分组新增 `force_openai_ultrafast` 开关；Composite 和其他平台归一为关闭，前端平台切换时也立即清除残留状态。
- 强制 Ultrafast 与强制 Fast 互斥，后端以 Ultrafast 优先；免费 Fast 保持独立，不作用于 Ultrafast。
- 只对 `gpt-6.1-sol` 与 `gpt-6-astra`（沿用项目已有别名规范化）强制出站 `service_tier=ultrafast`。
- 不区分 API Key、OAuth 或上游能力清单；命中后统一发送 Ultrafast，上游不支持时返回现有上游错误，不自动降级。
- 对命中分组规则的目标模型，全局 Fast/Flex 策略只允许 `block` 拒绝；`filter` 与 `force_priority` 不会把强制 Ultrafast 降级。
- 只有启用该开关的 OpenAI 分组、目标模型且最终 tier 仍为 Ultrafast 时，才对本次有效 Standard token 定价应用 8×覆盖；未启用分组沿用既有 Ultrafast 计费。
- 覆盖作用于克隆后的请求级定价对象，不修改全局模型目录、共享缓存或其他分组价格。
- 新增数据库迁移 `246_group_force_openai_ultrafast.sql`；认证缓存版本从 24 升至 25。

## 主要提交

- `718f9b457`：分组字段、迁移、Ent 生成代码、管理 API、复制、认证缓存与互斥归一化。
- `c261a20d8`：管理员 UI、类型、国际化、OpenAI-only 显示和 Fast/Ultrafast 互斥交互。
- `26dab2c3b`：HTTP、WS、Responses patch 请求策略及全局策略优先级。
- `c928288eb`：分组感知的 Ultrafast 8× token 计费与回归测试。
- 待提交补充：平台切换时清除不可见 Ultrafast 状态，并补充前端归一化回归测试。

## 实际验证

以下命令均在 E 盘功能工作树执行，并在同一次 PowerShell 调用中显式加载 `tools/project-env.ps1`：

- `go generate ./ent`：通过，生成 Ent 字段和迁移 schema 代码。
- `go test -tags=unit ./...`：通过；Windows 测试进程显式把 Git `usr/bin` 加入 PATH，以满足既有 `pg_dump` 测试夹具调用 `sh` 的环境前提。
- OpenAI Fast/Ultrafast、WS、计费和用量记录聚焦测试：通过。
- 前端全量 `vitest`：424 个测试文件、3607 项测试通过；包含本功能的分组和 i18n 测试。
- 前端 `vue-tsc -b`：通过。
- 前端 production build：通过；项目脚本通过临时 pnpm 9.15.9 shim 执行，直接构建器也通过；仅有既存 chunk size / dynamic import / Browserslist 提示，无构建错误。
- 前端 `pnpm install --offline --frozen-lockfile`：通过，复用 E 盘共享 pnpm store，未下载新包。
- 未执行数据库集成测试、Docker 构建或生产服务器操作；因此不能据此宣称迁移已在线执行或已发布。

前端验证复用了主项目 E 盘现有依赖目录的临时 junction，验证后已删除 junction；没有复制、下载或删除主项目依赖。

## 部署与迁移注意事项

- 发布前需在目标数据库执行应用现有迁移流程，使 `groups.force_openai_ultrafast` 存在；历史迁移未修改。
- 发布新版本前确认 Redis/API Key 认证缓存刷新，代码已通过快照版本 25 使旧快照失效重建。
- 本次没有构建或推送镜像，没有连接生产服务器；已发布的 `0.2.16-r1` 镜像不包含本功能。
