# 0.2.21-r4 Linux镜像发布

北京时间2026-11-13 23:40:23：公开镜像已发布并完成验证。由用户自行部署，本轮未连接或修改服务器。

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.21-r4
```

固定digest：`sha256:9a70273ffa66a46b441e1e5c28dc0c497993899afae5e75713fc2904f3e08627`。

- 应用源码：`974f727ca58caf74bc1b278df7ccac07664ab636`；发布控制：`d567ebd626ef1303e9fcd4bbea986ab8ecd82be7`。
- 仅原生`linux/amd64`和`linux/arm64`，Go1.27.4；无macOS构建、无v*标签、无服务器部署。
- [应用CI43450352935](https://github.com/Ttt599536561/sub2api/actions/runs/43450352935)、[应用Security43450352954](https://github.com/Ttt599536561/sub2api/actions/runs/43450352954)、[控制CI43450435407](https://github.com/Ttt599536561/sub2api/actions/runs/43450435407)、[控制Security43450435447](https://github.com/Ttt599536561/sub2api/actions/runs/43450435447)及[发布43453423791](https://github.com/Ttt599536561/sub2api/actions/runs/43453423791)全部成功。
- 双架构实际启动、版本/OCI来源及资源检查通过；五必要artifact的ZIP hash/size/CRC、精确文件集合及来源/版本/Go/架构绑定通过。
- 匿名核验24个镜像层，共92,682,721字节；14层14,254,294字节复用后重新hash与匿名HEAD，10层78,428,427字节下载后校验。OCI/Actions绑定、最终tag稳定检查全部通过，验证器exit0。

## 功能与升级

QQ邀请者奖励与被邀请者在原邀请群的资格绑定；未兑码永久作废，已兑按原授予凭据扣回原福利周期额度，保留用量、期限、其他奖励与付费权益；新增群内取消公告及邀请者私信、后台取消状态与通知状态。完整行为、并发与审查见[实现记录](../docs/changes/2026-11-13-qq-invite-revocation.md)。

相对r3新增258迁移，318→319；此前r2通常317→319，公开r1为314→319。先备份数据库和配置，只替换Sub2应用镜像；保留原邀请/生图加密密钥、PG/Redis卷、NapCat网络与会话，不重新初始化机器人。不要回退不理解撤销账本的旧应用或恢复作废码。

升级后处理收到的有效退群事件，已有映射奖励也适用；未上报的历史退群不能凭空追溯。历史兑换缺少授予凭据时明确进入人工核对，不猜测扣其他权益。未向真实QQ发送测试消息。

## 审查与证据

三方审查补齐跨群迟到入群误扣、通知/领取反向锁序、跨活动多码与兑换死锁，以及鉴权缓存可靠失效。真实PG回归先复现死锁再验证修复；最终全量CI/Security、PG18迁移与Integration均通过。

先前应用4a709da56/控制9e7a93542属于发布准备阶段；补正鉴权缓存后旧两CI取消、无失败job，不用于最终门禁。唯一r4发布固定上述最终来源，没有覆盖旧镜像。

本机证据：`.cache/qq-invite-revoke-20261113/publish/verification-0.2.21-r4/final-verification-summary.json`及关联provenance/registry摘要；共享依赖和已验镜像层缓存保留，无残留.partial。测试PG已安全停止、55473无监听。

自动审批拒绝本轮临时PG数据/环境及审查临时文件清理，仅返回`blocked by policy`，未提供具体原因；文件实际保留，未换手段重试。主目录原dist、调查文件与本机环境脚本均保全。

收尾补充：已核对本轮工作树无未提交差异且全部提交合入main；删除本轮依赖junction/临时工作树的命令同样在执行前被自动审批拒绝（blocked by policy），实际工作树与junction保留，未重试。主目录和远端main同步；必要源码与发布结论已归档，保留分支历史。
