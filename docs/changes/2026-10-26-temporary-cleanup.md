# 旧临时文件清理盘点（2026-10-26，未执行删除）

用户明确要求清理此前临时文件/临时测试产物。已盘点Sub2与独立检测站；两个原生PowerShell删除命令均被自动审批在进程创建前以 `blocked by policy` 拒绝，未提供具体规则。已停止删除，没有切换工具/方法或删除父目录绕过。**实际删除0文件、释放0字节。**

## 精确候选与保留边界

- Sub2：15个候选、7,628个文件，1,836,811,228字节（约1.71GiB）。这些是待处理清单，不表示所有目标已完成执行前检查。
- 检测站：80个建议候选、1,952,208字节；另27个目录/文件因证据或内容差异保留待核。两仓候选合计约1.84GB（十进制）。
- 保留源码/未提交修改、Git工作树、共享依赖与编译缓存、实际部署包和回退资料、验证摘要/manifest/before基线、Cloudflare授权与DPAPI管理凭据。
- 保留本机QQ验收应用及真实登录配置；另一个旧PG目录有残留PID且停止证据不足，未列删除。含重解析点的旧依赖树不整体清理，Docker安装/更新文件及不明用户文件不属于本次目标。

| Sub2相对目录/文件 | 字节 | 类别 |
| --- | ---: | --- |
| `.cache/tmp/go-build/go-build2306474907` | 12,850,184 | orphan_go_work_directory |
| `.cache/tmp/go-build/go-build2612489598` | 347,467,707 | orphan_go_work_directory |
| `.cache/tmp/go-build/go-build3063389129` | 606,615,614 | orphan_go_work_directory |
| `.cache/qq-release-review-20261025/pgdata` | 160,335,138 | stopped_isolated_test_pg |
| `.cache/affiliate-subdomain-20261020/pgdata` | 112,246,806 | stopped_isolated_test_pg |
| `.cache/review-1cfafd30e-20261018/build/sub2api-linux-amd64` | 161,082,887 | old_validation_binary |
| `.cache/review-1cfafd30e-20261018/build/sub2api-linux-arm64` | 150,121,128 | old_validation_binary |
| `.cache/review-1cfafd30e-20261018/frontend-build` | 6,385,925 | old_validation_frontend_build |
| `.cache/qq-image-generation-20261023/sub2api-qq-image-linux-amd64` | 183,405,458 | old_validation_binary |
| `.cache/qq-compact-command-20261025/binary.xor.gz` | 65,726,482 | unused_delta_candidate |
| `.cache/qq-compact-command-20261025/binary.delta.gz` | 28,874,398 | unused_delta_candidate |
| `.cache/qq-reply-editor-20261024/browser-harness` | 1,697,170 | completed_mock_browser_harness |
| `.cache/qq-release-review-20261025/workflow-test-remnants` | 0 | empty_test_fixture_directories |
| `.cache/qq-silent-parser-audit-20261025/silent_parser_audit_test.go` | 2,070 | completed_parser_test_fixture |
| `.cache/qq-silent-parser-audit-20261025/overlay.json` | 261 | completed_parser_test_overlay |

## 本次实际预检和拦截

1. `binary.xor.gz`：确认是未采用的传输候选，65,726,482字节，SHA-256与历史文件一致；尝试包含精确路径、非重解析路径及独占读检查的原生删除命令，被自动审批拒绝，命令未启动。未删除该文件或其父目录。
2. `.cache/tmp/go-build/go-build3063389129`：已实际完成路径包含关系、祖先/子树重解析点、进程引用和全部1,395个文件独占读检查，606,615,614字节；它是GOTMPDIR工作目录，不是共享GOCACHE。后续独立删除命令同样被自动审批拒绝、未启动。
3. 当前未发现相关Go/workerd/PostgreSQL测试进程；54392、55465、55449、16389、18089无监听。没有停止任何用户进程。检测站未再尝试删除。
4. 清理前后比对Sub2 20个与检测仓30个非缓存源码/WIP文件，内容保持；检测仓2份现行凭据文件指纹保持。这里只记录通过结果，不输出凭据内容或指纹。

## 后续接手

待审批条件允许执行删除后，再按精确清单重新核对占用、路径和文件变化；不能把本次用户授权当成绕过自动审批的理由。不要直接删除整个.cache或任务证据目录。历史文档中的blocked记录保留，本次记录也不是已清理成功声明。

完整候选与预检/拒绝回执在Sub2 `.cache/cleanup-20261026/`；检测仓报告在 `E:/CodexData/sites/ai-check-20261011/.cache/cleanup-20261026/`。原验证摘要和发布/回退文件继续保留。本轮仅新增清理记录，未提交/推送或改动线上服务。
