# 0.2.21-r1 发布前工具链修正

日期：2026-11-11（Asia/Shanghai）。用户已授权推送 GitHub main 并在 GitHub 构建 Linux 镜像；不构建 macOS，不部署服务器。

## 初次云端检查

合并及两轮审查修复源码 `29bb1ea621350f304860314837587220bfa54145` 已推送 main。CI：[42976473854](https://github.com/Ttt599536561/sub2api/actions/runs/42976473854)；安全检查：[42976473729](https://github.com/Ttt599536561/sub2api/actions/runs/42976473729)。后端 govulncheck 报告当前 Go 1.27.3 标准库受 GO-2026-6859 影响，安全门禁失败，因此该源码不作为最终镜像构建来源。

[官方漏洞记录](https://vuln.go.dev/ID/GO-2026-6859.json)指出 net/http 的 MaxBytesReader 相关响应头竞态可能导致 HTTP/1 服务崩溃，修复版本为 Go 1.27.4；[Go 发行记录](https://go.dev/doc/devel/release#go1.27.4)记录其于 2026-11-10 发布。

## 修正

统一升级 backend/go.mod、三个当前 Dockerfile、当前 CI/安全/通用发布工具链断言及开发入口说明至 Go 1.27.4。既有业务逻辑和模块依赖版本不变，不修改历史专用发行工作流的来源/工具链；本次新增 Linux 专用 0.2.21-r1 工作流使用新工具链并重新固定通过门禁的应用源码。未增加安全例外、降低门禁或覆盖旧标签。

## 验证与发布状态

本机已验证实际 Go 1.27.4；Linux/amd64 CGO0 嵌入构建及 build-info 通过，请求体限额三个专项通过，Linux govulncheck 普通/verbose 均 exit 0（0 可达漏洞、0 导入包漏洞）。仅有未导入 OpenPGP 的模块级提示 GO-2026-5932，未调整模块版本或添加扫描例外。11 个当前工具链入口一致、diff 检查通过。最终应用提交需重新通过其精确 main/push CI 和 Security 后才触发镜像发布；应用源码、发布控制提交、镜像 digest 分开记录。此阶段未发布 0.2.21-r1，也未更新任何服务器。

本机证据存于 `.cache/release-v0221-r1-20261111/`，全部在 E 盘并复用既有缓存；不保存凭据。
