# QQ 智商查询与生图统一帮助菜单

日期2026-10-23。用户要求单独@机器人时返回智商查询与生图两类指令。当前两个独立程序共用机器人：AI检测站的Cloudflare QQ Worker负责智商查询，Sub2隔离测试站负责生图。

## 设计与实施清单

菜单由既有Cloudflare QQ程序唯一负责，避免将智商截图业务复制到Sub2或新增跨服务凭证/调用。裸@、/帮助、/菜单和既有未知指令返回一条分组清晰的统一菜单，智商分组仍动态读取原站公开启用分组。明确的/识别群、/生图帮助、/绑定、/确认绑定、/生图、/任务、/重发在Cloudflare侧不入队、不回复，交由现有生图程序处理。

Sub2新增可选QQ_IMAGE_EXTERNAL_HELP=true，仅明确启用的实例把/帮助交给外部菜单；默认保留独立机器人的旧别名，裸@继续不回复。/生图帮助使用机器人配置即可，不解密上游生图Key；白名单、验签/可信WS、时效、配置版本/接收者Guard与去重不变。所有资金/生成命令继续走原凭据和授权校验。

- [x] TDD实现Sub2外部通用帮助模式与机器人配置帮助路径；覆盖默认兼容、专用帮助、未知收据/重复消息、范围限制和业务不放宽。
- [x] AI检测站独立仓库TDD修改QQ菜单/指令归属，保持原智商查询与普通聊天行为；部署仅QQ Worker。
- [x] 跨程序路由契约、真实PG与workerd离线回归、类型检查/dry-run、独立审查。
- [x] 更新Sub2测试应用及明确环境开关，再更新原QQ Worker；不修改Sub2正式站、AI网站、Cron或生成执行器，不重置用户配置/秘密，不做真实付费生成。
- [x] 核验双方健康与QQ连接、菜单构建产物、生产前后保护，分别维护两仓文档；保留数据/旧镜像用于回退。真实群消息由用户触发，本轮不代发测试消息。

当前状态：两侧已部署，真实群消息展示仍由用户发送新消息确认。本机仅在各自E盘仓库及各自.cache/output工作，Sub2主目录原11份WIP保留，不提交或推送源码。

## 本地验证与产物

Sub2专项314个测试pass事件、QQ真实PostgreSQL整包236个pass事件，均0失败/跳过；两批有重叠，不相加作为独立用例总数。qqimage/handler go vet通过。默认通用帮助兼容、外部模式静默、专用帮助不读provider密钥、重复/未知发送收据只回一次、无业务副作用和原权限边界均有回归。Cloudflare独立源码由其项目维护：136项QQ/截图离线测试、类型与Wrangler dry-run通过；WebSocket与Webhook均验证精确指令分工，不以msg_seq冲突代替唯一回复归属。

当前Sub2构建版本0.2.17-qq-menu.523a797dcb9d，源码快照523a797dcb9d207260c7d656c588bfd5e5bbecde47651efd68522f360ada3e61；Linux二进制SHA256 c583cfb4bcedf5f1770f16cd7785cdbe114c3f20df55a94f8702893ad33cda1c，150790304字节；压缩包SHA256 bfd84ec2412f0d24838aa11deb5cc216aefec10ed2b6bf5923a361ce75e37cc0，44275975字节。Go1.27.3/linuxamd64/CGO0/embed/trimpath，前端无改动，复用并验证上一版本dist。源码快照先于后续部署文档补记。当前HEAD仍c9a9209ea，不能当成包含新功能的提交。

运维只给测试应用增加QQ_IMAGE_EXTERNAL_HELP=true；四份Compose样例与.env.example默认false，不会自动更改其他部署。撤销配套前应协调菜单归属，不能让两个程序都响应/帮助。既有Cloudflare队列可能保留旧版本归一后的查询，结构仍兼容，按原240秒到期约束处理，不清空用户任务、不重新发送历史消息。

## 实际部署与保全

北京时间2026-10-23 23:49–23:50，测试应用已更新为 `0.2.17-qq-menu.523a797dcb9d` / `sub2-qq-test:menu-523a797dcb9d`，环境 `QQ_IMAGE_EXTERNAL_HELP=true` 已实际核对。只替换测试应用镜像与这一明确环境开关，不调用配置保存API；配置ID仍5，全部QQ配置行（含加密凭据）摘要、模型/价格/白名单/模式/生图开关保持原值。生图仍关闭。运行二进制SHA与构建一致；旧镜像 `sub2-qq-test:ws-742bbe8031ba` 保留。

临停备份位于 `/srv/sub2-qq-test/backups/menu-switch-20261023-154937`，dump621491字节，pg_restore目录检查通过，未做实际恢复。测试PG/Redis短暂停启但容器ID与数据保留，仅应用容器重建；app配置与安装锁未变。原主目录11份tracked WIP哈希全部保留，无提交/推送。

独立Cloudflare QQ Worker `oneapi-observer-qq` 于北京时间23:51:37发布版本 `61f65219-54cb-4f8f-b7b5-af58e3324f59`，部署 `9f90ee4c-8694-40f3-ad42-1a6e40580444`，读回流量100%。health为configured=true、browser_bound=true、sandbox=false；原机器人密钥、vars和DO迁移未变。只部署QQ Worker，没有发布AI检测网站、Cron或生成执行器。其源码与文档分别保存在独立AI检测站仓库，未复制进本项目，也未提交/推送。

23:55:58复核测试站同一连接仍connected，last_connected_at为23:49:51.845594052，心跳推进到23:55:21.523135953。生产三个容器的ID、镜像、启动时间、重启次数与运行状态、三份生产配置stat和Nginx哈希不变；双域证书指纹与健康200保持。测试站QQ配置、settings、orders、grants、用户数、余额/冻结计数均保留。用户整行摘要变化，经与临停备份逐列比对仅updated_at、last_login_at、last_active_at三个活动时间改变，未将整行误报为不变。

没有代发QQ群消息或发起真实截图、模型生成、绑定/资金测试。Cloudflare受保护的队列/网关状态本轮未读取，不以health代替真实送达；没有重置管理员token或另建诊断服务。旧队列不清空，沿用原240秒有效期与去重，新消息按新版路由。用户在群里重新实际@机器人，或发送/帮助、/菜单，即可进行最终群内展示确认；不要复用旧QQ消息作为新菜单证据。

验证摘要位于主项目output/qq-unified-menu-20261023，包括artifact-summary.json、artifact-verification.json、preservation-verification.json、runtime-verification.json；原始测试和更新记录在对应.cache。Cloudflare实际发布与源码摘要在AI检测站自身output/.cache/qq-unified-menu-20261023。

自动审批拒绝清理AI检测站本次两个Miniflare临时运行目录，仅给出blocked by policy；已原位保留，没有通过其他方式重试。验证日志和必要构建/回退资料保留。该清理限制不影响部署及功能验证。

更新验证后已精确清理本轮远端上传临时压缩包44275975字节；远端release、新旧镜像、备份、本机部署包和验证摘要保留。该清理不涉及被拒的Miniflare目录。
