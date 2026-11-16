# 0.2.21-r5 Linux镜像发布

北京时间2026-11-16 19:55：公开镜像已发布并完成验收。用户授权推送GitHub并构建公开镜像，服务器由用户自行替换；本轮未连接服务器、未构建macOS、未创建触发通用发行的v*标签。

## 发布范围与当前状态

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.21-r5
```

固定digest：`sha256:8fb77b84fcf4f90ca593b25725138b7da8ddffddf79afd2cb46ab50b7065a296`。仅原生linux/amd64和linux/arm64；已验证匿名可拉取。

已推送并固定应用源码：`8eb4fe13a3215248cbf67e35586df9aa692b51aa`；发布控制：`f04d7f048396568158687a865ede4cb509bf0c25`。两SHA各自最新完整CI与Security Scan均成功，专用[发布运行43860688482](https://github.com/Ttt599536561/sub2api/actions/runs/43860688482)的gate、双架构构建/原生运行及publish全部success，attempt 1，19:52:58完成。

原生容器版本/OCI来源、Go1.27.4、PostgreSQL客户端、模型价格资源、setup状态和首页静态资源均通过；迁移253–260、福利补充账本/额度归属/分组保护及UI标识齐全。五必要artifact的ZIP hash/size/CRC及精确文件集合通过，每架构provenance七文件；两份官方Docker辅助构建记录按精确run/source/control/metadata单独列入allowlist，不作为发布验收证据。

匿名验收24层，共92,770,830字节；14层14,254,294字节复用缓存后重新hash及匿名HEAD，10层78,516,536字节下载校验。两平台config/manifest/index、Actions架构digest绑定、所有运行层可取及最终tag稳定复核全部通过，验证器exit0。

门禁记录：[应用CI](https://github.com/Ttt599536561/sub2api/actions/runs/43857689086)、[应用Security](https://github.com/Ttt599536561/sub2api/actions/runs/43857689015)、[控制CI](https://github.com/Ttt599536561/sub2api/actions/runs/43858219452)、[控制Security](https://github.com/Ttt599536561/sub2api/actions/runs/43858219412)。四项均为attempt 1，最终复核于北京时间19:45:54成功。

## 功能和升级

- 后台兑换码管理按邀请活动生成福利补充码，可给同一有效福利月卡增加月总额度或有效天数，保留已用量；普通订阅与旧QQ兑换规则保持。
- 活动分组按稳定ID绑定，相关引用阻止删除及改非订阅类型；旧弹窗在草稿换组后不能静默向新组生成码，冲突后刷新并要求重新选择。
- 手动加额归属独立，QQ退群不会误扣新增补额；管理员明确削减后，后续事件不会恢复已削减额度。
- 相对r4新增259/260迁移，319→321；相对r3为318→321。先备份数据库与部署配置，仅替换Sub2应用镜像，保留PG/Redis卷、QQ_INVITE_ENCRYPTION_KEY、QQ_IMAGE_ENCRYPTION_KEY、NapCat网络及会话。
- 不重新初始化机器人；新码生成/使用后不能盲目回退不理解新字段和账本的旧应用。没有执行真实发码、兑换、QQ发消息或生产数据调整。

实现及本地验证：[福利补充码与分组保护](../docs/changes/2026-11-16-welfare-redeem-effects.md)、[P2绑定修复](../docs/changes/2026-11-16-welfare-campaign-stale-fix.md)。公开构建与部署状态分别记录；本文件的发布准备不表示服务器已更新。

## 证据位置

本机工作和验收缓存：`.cache/publish-v0221-r5-20261116`，最终摘要为`verification-0.2.21-r5/final-verification-summary.json`及关联provenance/registry摘要。验收器适配42项离线测试通过，发布控制通过独立只读审查；最终终审独立重算五产物及24层hash并核对GitHub jobs、来源和平台，结论通过。关键结论已归档本文。

本轮仅使用GitHub Linux runner构建，本机所有验收产物、镜像层缓存实际留在E盘并复用旧缓存。296份原未跟踪文件经SHA256核对全部保全；没有服务器更新或真实业务操作。
