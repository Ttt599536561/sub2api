# 0.2.17-r4 发布前最小安全修复

日期：2026-10-23（Asia/Shanghai）。稳定提交主题：`fix: update Go and redirect dependency security patches`。

用户先要求在刚推送的卡片源码 CI 通过后发布 r4；安全门禁发现真实告警后，用户明确批准先补两项最小安全修复，以通过检查的新提交发布。没有增加安全豁免，没有引入其它功能。

## 原始失败与修复

原卡片功能提交 `6f90164d5b55584f8266764d2d26f5c6a9bd1a9d` 的 [Security Scan 39829164159](https://github.com/Ttt599536561/sub2api/actions/runs/39829164159) 失败：

- Go crypto/x509 报 [GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809)，Go1.27.2受影响，修复版本1.27.3；扫描显示Passkey登录到Certificate.Verify的可达路径。静态可达报告不代表已证实生产可利用。
- axios1.20.0引用的follow-redirects1.16.0报 [GHSA-q3jh-wrc6-jgq8](https://github.com/advisories/GHSA-q3jh-wrc6-jgq8) 和 [GHSA-8r9p-f939-6h3c](https://github.com/advisories/GHSA-8r9p-f939-6h3c)，两个公告均将1.16.1列为修复版。

变更范围：backend/go.mod、三个Dockerfile与CI/release/security版本断言统一为Go1.27.3（7文件8处版本值）；frontend/package.json新增精准override `follow-redirects@<1.16.1: 1.16.1`，pnpm9更新锁文件。锁文件除了新增override和该依赖版本/完整性/引用外保持相同，axios及其他包版本不变。go.sum、业务Go源码、SQL/Ent与审计豁免无变更。

主目录DEV_GUIDE和其它原WIP不修改；本轮使用独立E盘工作树与项目共享缓存。当前执行工具链基线以go.mod的1.27.3及本记录为准，旧开发指南/历史验证中的1.27.2不作为本轮发布工具链。

## 验证与发布门禁

本机Go1.27.3实际版本已核对，govulncheck v1.9.0通过：0个可达漏洞、0个已导入包漏洞，另有7个依赖模块的未调用漏洞提示。

pnpm9冻结安装、前端类型检查和全量lint通过；生产依赖审计中的follow-redirects告警已消失，剩余2项high为已有xlsx例外，现有审计检查器通过，没有新增或扩大豁免。其余前端验证与新提交精确SHA的GitHub CI、安全扫描结果在后续发布记录归档，不把原提交失败结果标成成功。

发布必须使用安全修复后的新源码提交，固定SOURCE_COMMIT、应用Commit和OCI revision；禁止只换构建环境却继续声称原SHA通过。新SHA的main/push最新CI与Security Scan全部成功后，才能构建amd64/arm64并合成r4索引。保持最终版本标签不可覆盖。

相对生产r3，本轮不新增数据库迁移或必需环境变量。Go补丁改变对已不可信中间证书的处理。没有部署生产，实际发布事实以后续r4记录为准。

本轮本地前端最终全量为433文件3718项通过，生产构建及其3项i18n检查通过。首次嵌套pnpm调用误用了本机默认pnpm12而退出；通过仅进程PATH固定pnpm9后，原构建脚本通过，未修改项目脚本或全局设置。依赖冻结安装复用981包，仅下载修复后的1包；临时shim已清理。前端日志、锁差异收据与Go扫描在主项目.cache/publish-monitor-r4-20261023，关键结果已归档本页。

## 后续镜像发布

2026-10-23已将卡片功能和获准最小安全补丁作为源码`76ff7c88a0e792b39936346db9c9707930eba0ef`发布为0.2.17-r4，amd64/arm64均通过检查；CI、安全扫描、发布和匿名运行层核验均成功。前面阶段记录的未发布状态描述当时事实，当前发布身份与升级步骤见[r4发布记录](../../deploy/CUSTOM_IMAGE_0.2.17-r4.md)。本轮未部署生产。
