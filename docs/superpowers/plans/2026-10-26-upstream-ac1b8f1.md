# 合并 upstream ac1b8f1 与已上线二开代码并发布 Linux r6

> 执行方式：本会话隔离工作树实施，独立代理分域审查，主代理整合并负责发布；用户已授权合并、修复、推送及GitHub构建。

**目标：** 固定上游 ac1b8f1271be0d674fa6f074403c1410f30a3a81，保留二开功能，纳入已上线QQ原图投递修复，发布可追溯Linux镜像。

**工作区：** E:/sub2二次开发项目/.worktrees/merge-ac1b8f1-20261026；共享缓存 E:/sub2二次开发项目/.cache，任务证据位于 merge-ac1b8f1-20261026。

- [x] 核对主目录 cda70a59b、上游目标、origin和线上源码manifest；34份现有改动/非忽略文件备份并记录SHA256。
- [x] 确认公开r5、正式站最后记录r4与QQ测试站inline修复三个身份；QQ 11文件与部署manifest逐字节相符，其余Codex/CCSwitch WIP保全。
- [x] 合并前关键包基线 go test -tags=unit ./internal/qqimage ./internal/service ./internal/repository ./internal/handler；定位已有QQ测试panic并回归。
- [x] git merge --no-commit --no-ff ac1b8f1；逐项解决6文件冲突：chat/responses handler保留二开反馈与上游断流计费，antigravity用量转换保留非负保护，billing测试合并两侧断言，package/lock保留安全版本。
- [x] 纳入11份已上线QQ源码/测试及定位文档；不纳入无上线证据的Codex/CCSwitch/历史草案WIP。
- [x] 独立审查后端流式/调度/计费、前端/依赖/发布、QQ与迁移；确认福利、推广、ultrafast、监控、QQ功能仍在。
- [x] 本地完整unit、真实PG QQ与迁移升级回归、Go vet/lint/security、前端完整test/typecheck/lint/build；必要修复先复现再回归。
- [x] 维护CHANGELOG/HANDOFF/本次changes；精确源码提交，保全主目录WIP后fast-forward回main并推送；核对origin SHA。
- [x] 精确源码GitHub CI与Security Scan成功；新增r6专用workflow固定SOURCE_COMMIT，Ubuntu原生amd64/arm64，禁止v标签触发通用多平台release。
- [x] 触发Linux发布，核验工作流成功、artifact/digest/OCI来源与两架构；归档最终镜像地址，文档提交推送。
- [x] 校验主目录WIP保全，停止本次临时测试进程，清理已不用的本次临时文件，保留必要证据与缓存。

验证限制：本任务发布镜像，不部署正式站/QQ测试站、不发送QQ或真实模型请求。新246迁移会启用已有Gemini/Antigravity分组生图权限，须验证并写入升级注意事项；按完整文件名识别迁移，不重命名已发布历史SQL。

完成结果：应用02f706a19、控制69b52de91，CI40303821798/安全40303821916/发布40305430958均success。r6匿名24层与来源校验通过，详见deploy/CUSTOM_IMAGE_0.2.17-r6.md。清理项按实际边界完成：测试进程停止、Docker恢复原状态；pgdata删除被自动审批blocked by policy拒绝，保留未绕过，原WIP和缓存不清理。

补充清理边界：Git注销本轮隔离工作树后仍有Windows目录残留，原生清理被自动审批blocked by policy拒绝；保留原位，不再尝试，代码已在main且分支保留。
