# 0.2.21-r4 Linux镜像发布

状态：准备发布，尚未确认镜像构建成功；用户自行部署，不连接服务器。

- 目标镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.21-r4`。
- 应用源码：`4a709da569863edc429613b04511eba559b94ef8`。
- 平台：仅原生Linux amd64/arm64，不触发macOS工作流，不推v*标签。
- 专用工作流：`.github/workflows/publish-custom-v0221-r4.yml`；发布前应用与控制SHA必须通过完整CI/Security，禁止覆盖已有最终tag。
- 功能：QQ邀请退群月卡取消/原周期额度追回、群与邀请者私信通知、不可复活与并发保护。详见[实现记录](../docs/changes/2026-11-13-qq-invite-revocation.md)。
- 相对r3新增258，迁移318→319；此前r2通常317→319，公开r1为314→319。备份后保留原密钥、PG/Redis卷、NapCat网络与会话，只替换Sub2镜像。
- 不将镜像发布等同线上部署；升级后处理收到的退群事件，未上报的历史退群不能自动追溯。缺少旧兑换授予凭据的记录明确进入人工核对。

构建运行、digest与五份artifact/匿名层验证证据在实际完成后追加。本机缓存路径`.cache/qq-invite-revoke-20261113/publish`。
