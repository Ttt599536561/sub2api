# Sub2API 0.2.20-r3 草稿活动月卡分组修复

## 已发布并完成生产更新（2026-11-11，Asia/Shanghai）

镜像`ghcr.io/ttt599536561/sub2api-custom:0.2.20-r3`，固定内容`ghcr.io/ttt599536561/sub2api-custom@sha256:24451524ff6e31be5a2705d2e08fdd77dd997826f07e5f568a4b1bb762d0e066`。应用源码`33961e3b57f94535c71aa57e99d81f329aa976e9`、发布控制`ed7d5bfe77205537af76effc175abf9f96b4a0dc`；文档收尾不改变镜像内容。

- 精确应用[CI 42879495475](https://github.com/Ttt599536561/sub2api/actions/runs/42879495475)与[Security 42879495228](https://github.com/Ttt599536561/sub2api/actions/runs/42879495228)全部成功；控制[CI 42879679939](https://github.com/Ttt599536561/sub2api/actions/runs/42879679939)与[Security 42879679934](https://github.com/Ttt599536561/sub2api/actions/runs/42879679934)全部成功。
- [发布42882348334](https://github.com/Ttt599536561/sub2api/actions/runs/42882348334)成功，linux/amd64和linux/arm64均实际运行验证，包含原NapCat和新增草稿换组标记。
- 03:14:49完成5份ZIP产物的大小、SHA256、CRC、文件集合及run/control/source/arch绑定验证。03:15:19完成匿名镜像验证：24个唯一运行层、92,415,703字节，其中10层新下载、14层复用并重验。独立终核无阻断。
- 03:16:44已按用户授权通过SSH更新生产，只替换Sub2镜像。Sub2、PostgreSQL、Redis、NapCat均健康；本机及公开HTTPS健康检查均200，Sub2内部OneBot认证调用成功且指定QQ仍在线。数据库、Redis、NapCat容器ID及原环境、密钥、端口、数据挂载、网络网关保持。
- 升级前数据库custom备份882,913,813字节，应用/新配置归档80,135,231字节；pg_restore目录及完整解码通过，未进行恢复演练。未修改活动、库存或邀请记录；验收时原活动仍未启用、库存/记录/奖励为0，全局接收维持用户已开启状态。

本次无需重新扫码；管理员刷新后台后可修改符合条件的空草稿分组。已启用过或有库存/邀请历史的活动仍锁定套餐，并通过真实reason字段显示明确原因。没有数据库迁移，没有发送QQ测试消息或发放月卡。原主目录24项未提交改动保留，不纳入镜像。

原始证据在主项目`.cache/napcat-campaign-fix-20261111/`；服务器备份及含连接身份的回执保持私有。下文保留候选准备及取消记录，不代表当前版本尚未发布。

## 修正候选准备（2026-11-11，Asia/Shanghai）

固定最终应用源码`33961e3b57f94535c71aa57e99d81f329aa976e9`，包括草稿换组、库存互斥、保存版本接纳及真实API reason字段修复。最终本地6文件89项前端测试、真实客户端链路、类型检查、lint、构建、后端JSON响应专项和独立复核均通过。已推送main，等待该SHA完整CI/Security后重新构建；不复用首次候选的云端检查。此阶段r3尚未发布或部署，生产仍为r2。

## 首次候选准备（已在发布前取消）

首次候选源码 `1fb194e877c288c1a1beffac0e5bed7e6d11ea8f`；目标镜像 `ghcr.io/ttt599536561/sub2api-custom:0.2.20-r3`，仅Linux amd64/arm64。此阶段尚未发布或部署r3；服务器仍为r2。功能范围与本地实测见[修复记录](../docs/changes/2026-11-11-napcat-campaign-group.md)。

专用工作流固定上述源码并沿用精确main/push CI与Security成功门禁、双原生Linux运行校验、不可覆盖最终tag及五份验证产物。每架构provenance新增campaign-group-package-checks.txt，要求实际镜像二进制包含专用分组锁定错误码；保留NapCat账本、迁移和UI标记。验证工具继续校验全部ZIP文件集合、大小、SHA256及run/control/source/arch绑定，并匿名核验manifest/config和全部运行层。

## 升级与兼容

无数据库迁移，无NapCat镜像变更。保留现有活动、码池、记录、会话及全部加密密钥。仅未启用且从未启用、无库存及邀请历史的草稿可改订阅分组；已有库存或曾启用的活动需新建活动更换套餐。版本冲突仍需重新加载，业务锁定显示具体原因。

生产更新必须沿用当前Compose项目、原主文件及NapCat overlay，只更新Sub2应用，保留数据库/Redis、端口、卷、网络优先级与QQ密钥。升级前检查在途任务并备份；升级后确认健康和原QQ账号在线。回退r2会恢复原草稿分组不可修改限制，不应清空或改写数据。本版验收不通过发送真实QQ消息发奖进行。

首次构建42877780986在最终标签发布前取消：真实接口错误原因字段不匹配仍需补正。r3最终标签经匿名读取确认未发布；新固定源码及验证在后续控制提交更新。原完整CI检查不能替代补正后的精确源码门禁。
