# 02c68becd 复审问题修复

日期：2026-11-07（Asia/Shanghai）。修复此前只读审查确认的两项P2，基于已同步main的 `d17a40d261b900d62dcba544e12df205f0b8a159`（应用代码 `02c68becd6c392eaa4eab48341c64fca2c6d5030`）。主目录原23项WIP、5份其他未跟踪文件和293份dist共321份已重新备份，继续独立保全。

## 修复范围

1. **MCP撤销显式Read Committed。** `RevokeUserGrants` 原来继承数据库/角色默认事务隔离级别，却依赖第二条语句取得新快照。在默认Repeatable Read时，正在等待用户锁的撤销看不到刚提交的授权，返回成功后授权仍能兑换Token并认证。现在仅此撤销事务显式使用 `sql.LevelReadCommitted`，保持用户锁、单调时间戳、user→grant顺序和回滚语义；不修改全局数据库配置。默认Read Committed对照与非默认Repeatable Read真实服务回归均要求撤销后Token交换失败。
2. **导出失配时提供安全恢复入口。** 保留挂载时的账号/会话owner，不把旧页面或已开始的导出重新绑定到新登录。检测登录变化时取消请求/429等待，禁用导出并显示明确提示和整页重新加载按钮；重新加载重建全部身份与页面状态。原页面普通刷新不得清除失配提示；同会话Token轮换仍可正常导出。补中英文文案和真实Vue/Axios回归。

不改计费规则、数据库schema、已有MCP授权有效期或JWT全局撤销设计；本轮不发布镜像或部署线上。

## 验证记录

最终代码验证已完成，证据目录：`E:/sub2二次开发项目/.cache/fix-02c68becd-20261107`。

- 后端unit：exit0，61个测试包，28,654个PASS事件，0失败，18项条件跳过。
- Docker PostgreSQL/Redis完整integration：exit0，55个测试包，17,252个PASS事件，0失败，16项条件跳过。跳过项为外部凭据、专用审计PG/Redis、既有TODO等，未冒充执行。
- MCP永久服务回归明确取得RED：RC通过、继承RR时失败；固定RC后，两种DB默认配置均无法兑换已撤销授权的Token。原SQLmock顺序及MCP回归通过，未升级依赖或引入无必要mock驱动。
- 前端永久回归先取得10处预期失败，再通过目标48项；完整461文件/4,356项通过。typecheck、lint、i18n、vue-tsc和Vite生产构建均成功，仅保留既有chunk-size提示。
- 独立终审通过：真实PG RC/RR两叶和前端45项独立复跑均通过，复核owner不重绑、旧任务取消、提示持续及全页恢复；六份审查文件在验证期间校验值不变。
- 修改范围backend repository的Linux golangci-lint为0 issues；嵌入前端的web/cmd测试、Linux amd64构建通过。此次未改依赖锁或计费代码。

所有命令加载主项目环境，联网通过本机7900代理；Docker VHD已确认位于E盘。本轮独立PG容器仅绑定127.0.0.1:55470，密码只存在进程和测试容器环境，不创建新的主机凭据文件。前端使用pnpm9和既有依赖缓存。

## 整合状态

- 应用修复 `2f398e371e58ed7375f605b0d71f44d515fd6742` 已快进合入本地主目录并推送GitHub main，完整SHA已核实一致。
- 主目录23项tracked WIP按开工快照三方回放；5份其他未跟踪文件与293份dist哈希不变，备份及stash保留。WIP不进入远端提交，主目录恢复后的Codex专项和CC Switch 26项通过。
- 精确应用[CI 42371095920](https://github.com/Ttt599536561/sub2api/actions/runs/42371095920)已全部成功（unit、integration、前端、lint、部署脚本及发布辅助），[Security Scan 42371095918](https://github.com/Ttt599536561/sub2api/actions/runs/42371095918)已成功；已于2026-11-07核实完整工作流成功。后续交接记录提交仅更新Markdown，不改变已验证应用代码。
- 独立Docker PostgreSQL容器及测试卷已停止并删除，55470端口无监听。本轮没有创建主机凭据文件；验证摘要、必要恢复备份及共享依赖缓存保留。没有操作生产数据、发布镜像或部署服务器。
