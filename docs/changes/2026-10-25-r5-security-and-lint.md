# 0.2.17-r5 发布门禁补充修复

日期：2026-10-25（Asia/Shanghai）。稳定变更主题：`fix: satisfy QQ release security and lint gates`。功能提交 `4efa5ae4e410f603c60dd84f8a15555e16aa1984` 已整合main并推送；该SHA的Linux CI后端unit/integration、前端、shell和发布辅助通过，但golangci-lint与Security Scan未通过，因此没有触发r5镜像发布。

## 依赖安全

- QQ新增WebP解码让既有 `golang.org/x/image v0.41.0` 的三项漏洞可达：GO-2026-6222、GO-2026-5061、GO-2026-4961。仅升至官方最低覆盖版本v0.45.0；现有x/sys0.48和x/text0.42已满足传递要求，其他模块及Go1.27.3不变。go.sum仅增加目标版本两行校验和。
- DOMPurify3.4.14触发GHSA-cg2g-xh64-v4q7及GHSA-mrv2-3pqr-j5jr门禁。direct及override精准升级到官方修复版本3.4.17，锁文件只有目标包相关引用变化；没有新增审计例外。
- 官方来源：[Go 6222](https://pkg.go.dev/vuln/GO-2026-6222)、[Go 5061](https://pkg.go.dev/vuln/GO-2026-5061)、[Go 4961](https://pkg.go.dev/vuln/GO-2026-4961)、[DOMPurify公告1](https://github.com/cure53/DOMPurify/security/advisories/GHSA-cg2g-xh64-v4q7)、[公告2](https://github.com/cure53/DOMPurify/security/advisories/GHSA-mrv2-3pqr-j5jr)。

后端修复前扫描exit3，修复后Linux amd64全仓govulncheck exit0、可达和已导入包漏洞0，仍有2个未调用模块漏洞提示。真实PG QQ域400个PASS事件、0fail/skip；上游WebP22个PASS事件，VP8L包无测试文件（不计为独立测试通过）。前端生产审计按现有门禁通过、DOMPurify findings0；5个官方公告形态离线场景、17文件318项相关回归、类型、全量lint、生产构建通过。pnpm9生成锁、本机pnpm10冻结安装复用缓存且只下载1包，锁哈希未变；GitHub/Docker继续pnpm9验证。最初并行lint碰到Vitest临时配置删除的ENOENT，串行复测通过，未修改源代码绕过。

## Go lint质量门禁

CI默认展示35条；本地关闭展示截断后共有72项（69errcheck、2staticcheck、1unused）。关键SQL前置、Scan、Commit及测试退款错误改为真实检查；既有Close/Rollback清理显式处理，竞争测试仅接受预期ErrConflict失败方并保留最终钱包断言。公钥与测试payload类型断言补fail-closed检查；删除确无调用的旧imageHelp。没有nolint、规则放宽或账务/重试/鉴权行为改变。

同CI规则、GOOS=linux/GOARCH=amd64、golangci-lint2.14全仓为0 issues；真实PG QQ+handler两包2,480个PASS事件、0失败、1个既有钉钉条件跳过。19个Go文件由另一代理独立逐项复核，无阻塞发现。测试总数在不同批次间有重叠，不相加。

本轮源码、测试及记录一同提交，之后必须在新固定SHA上重新通过完整Linux CI与Security Scan才允许发布镜像。原始失败运行：[CI 40182343836](https://github.com/Ttt599536561/sub2api/actions/runs/40182343836)、[Security Scan 40182343841](https://github.com/Ttt599536561/sub2api/actions/runs/40182343841)。新提交SHA在实际提交后补录或按本主题追溯，不预写。

本机证据在主项目 `.cache/qq-release-review-20261025` 的x-image-security、dependency-security及lint摘要。主目录原WIP已恢复：15个未重叠文件哈希不变，.gitignore与HANDOFF合并保留双方内容，原邮件排查/设计草案完整保留；原stash和备份继续保留。没有运行macOS任务，没有部署生产，也没有代发QQ/邮件或调用付费生成。
