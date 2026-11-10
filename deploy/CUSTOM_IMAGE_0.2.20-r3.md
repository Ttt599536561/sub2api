# Sub2API 0.2.20-r3 草稿活动月卡分组修复

## 发布准备（2026-11-11，Asia/Shanghai）

固定应用源码 `1fb194e877c288c1a1beffac0e5bed7e6d11ea8f`；目标镜像 `ghcr.io/ttt599536561/sub2api-custom:0.2.20-r3`，仅Linux amd64/arm64。此阶段尚未发布或部署r3；服务器仍为r2。功能范围与本地实测见[修复记录](../docs/changes/2026-11-11-napcat-campaign-group.md)。

专用工作流固定上述源码并沿用精确main/push CI与Security成功门禁、双原生Linux运行校验、不可覆盖最终tag及五份验证产物。每架构provenance新增campaign-group-package-checks.txt，要求实际镜像二进制包含专用分组锁定错误码；保留NapCat账本、迁移和UI标记。验证工具继续校验全部ZIP文件集合、大小、SHA256及run/control/source/arch绑定，并匿名核验manifest/config和全部运行层。

## 升级与兼容

无数据库迁移，无NapCat镜像变更。保留现有活动、码池、记录、会话及全部加密密钥。仅未启用且从未启用、无库存及邀请历史的草稿可改订阅分组；已有库存或曾启用的活动需新建活动更换套餐。版本冲突仍需重新加载，业务锁定显示具体原因。

生产更新必须沿用当前Compose项目、原主文件及NapCat overlay，只更新Sub2应用，保留数据库/Redis、端口、卷、网络优先级与QQ密钥。升级前检查在途任务并备份；升级后确认健康和原QQ账号在线。回退r2会恢复原草稿分组不可修改限制，不应清空或改写数据。本版验收不通过发送真实QQ消息发奖进行。
