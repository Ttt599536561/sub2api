# 0.2.21-r6 Linux镜像发布

北京时间2026-11-17 20:24：公开镜像已发布，双架构运行与匿名拉取验收通过。用户授权推送GitHub并构建发布镜像；本轮未连接或更新服务器。

## 镜像与固定来源

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.21-r6
```

固定digest：`sha256:4b1bac21afd1f3c0335e0abbc564a19408bfa88c753b087fb64144ff5590b215`。支持原生`linux/amd64`和`linux/arm64`，已验证匿名可拉取。

应用源码：`2516693b3ad777b13d7c93c25e9c41b9b00fd25a`；发布控制：`df1e9dc35211488ca6b56c9170cdd13aaa521d89`。两SHA各自最新完整CI和Security Scan全部成功，专用[发布运行44081227366](https://github.com/Ttt599536561/sub2api/actions/runs/44081227366)的gate、双架构构建/原生运行及publish全部success，attempt 1，20:19:27完成。没有创建通用发行的v*标签或构建macOS。

门禁：[应用CI](https://github.com/Ttt599536561/sub2api/actions/runs/44078316286)、[应用安全](https://github.com/Ttt599536561/sub2api/actions/runs/44078316335)、[控制CI](https://github.com/Ttt599536561/sub2api/actions/runs/44078411403)、[控制安全](https://github.com/Ttt599536561/sub2api/actions/runs/44078411482)。四项均attempt 1成功；发布前再次按精确SHA、main和工作流核验最新成功记录。

## 功能与升级

- 管理员在兑换码管理指定订阅分组，可选择增加一次性额度或延长有效天数，同组有效普通及福利月卡均适用，不再要求邀请活动。
- 基础日/周/月额度优先消费，不足部分才扣补额；窗口重置不重复赠送。加时保留已用量及剩余补额；无有效订阅、错组、暂停、关闭等拒绝时不核销兑换码。
- 未使用的旧福利补码兼容通用规则，已使用旧码的历史收据保留。补额账本、PG持久预占和异步视频创建时计费身份隔离订阅期次，防止续期后扣错额度。
- 相对r5新增261/262/263迁移，321→324。升级前备份数据库与部署配置，仅替换Sub2应用镜像，保留PG/Redis卷、QQ_INVITE_ENCRYPTION_KEY、QQ_IMAGE_ENCRYPTION_KEY、NapCat网络和会话。
- 新效果和账本投入使用后，不盲目回退不理解新数据的旧应用。既有异步扣费故障没有通用持久重试队列；持久预占不等于完整收费故障恢复。

功能及本地验证见[通用订阅补充码记录](../docs/changes/2026-11-17-subscription-supplements.md)。用户指定账号的线上具体数据未核实，本次发布不写真实用户余额，也不代表生产服务器已经升级。

## 发布验收

原生容器版本、OCI源码与版本标签、Go1.27.4、golang.org/x/image v0.45.0、PostgreSQL客户端、模型价格资源、setup状态及首页静态资源均通过。打包检查覆盖历史QQ/福利兼容及261–263补额、预占、视频身份；每架构provenance精确八文件。

五项必要artifact的ZIP hash、大小、CRC、无重复文件和精确集合全部通过。两份官方Docker辅助构建记录按精确run/source/control/metadata单独列入allowlist，不作为运行验收证据。

匿名验收24个运行层，共92,854,472字节；14层14,254,294字节复用E盘缓存后重新hash和匿名HEAD，10层78,600,178字节新下载校验。index、两平台config/manifest、Actions架构digest绑定、全部运行层和最终tag稳定复核通过，验收器exit0。

## 全量CI发现并修正的问题

- 移除已被通用补码事务替代且没有调用者的旧`applyWelfareSupplement`，完整生产lint恢复0 issues；旧客户端生成、未用码归一化和已用历史读取保留。
- 严格订阅API契约样例补齐三个新增补额零值字段，保留旧字段与敏感信息断言；11项契约子用例通过。
- 日重置/过期续期测试夹具遗漏261历史账本清理，导致RESTRICT外键拒绝删除却吞错，污染后续用户测试。仅按夹具订阅ID补齐子表清理及错误断言；同进程红复现后，完整repository集成1868 PASS事件、0失败、41既有跳过，独立复审通过。首次本机整包有3个备份测试因PATH缺sh失败，补当前进程工具路径后整包通过，生产代码未因此改动。最终两SHA GitHub完整集成均通过。

前轮失败证据保留：应用[44074801518](https://github.com/Ttt599536561/sub2api/actions/runs/44074801518)、控制[44074888211](https://github.com/Ttt599536561/sub2api/actions/runs/44074888211)；这些失败提交未用于发布。最终镜像来源以上方固定SHA为准。

## 证据位置

本机证据目录`.cache/publish-v0221-r6-20261117`，包含四门禁、发布run/jobs、五ZIP、provenance与registry摘要。验收器56项离线测试通过；最终来源/工作流47项静态复审通过；20:26独立终审70项通过，重新核对实时GitHub门禁/发布、五ZIP及全部24层hash/大小，并再次匿名读取正式tag确认digest稳定。最终汇总位于`verification-0.2.21-r6/final-verification-summary.json`。

所有本机缓存和验证产物在E盘并复用旧镜像层缓存；原有4个未跟踪文件SHA256保全。本轮测试容器自动回收，原有6个容器未变。上一阶段被自动审批拒绝清理的工作树和文件保持原状；本轮无服务器或真实业务操作。
