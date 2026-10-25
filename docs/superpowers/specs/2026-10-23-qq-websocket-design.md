# QQ WebSocket 接入设计

日期2026-10-23。用户明确选择保留WebSocket、补齐接入并更新隔离测试站；不改变正式站，不提交/推送/发布公共镜像。沿用当前QQ工作树和现有配置、数据库、加密密钥，不清库。

## 选择与边界

采用内置长连接接收器，复用现有QQ命令和资金处理。另起桥接服务会增加凭据副本、伪签名边界和运维负担；切HTTP回调已被用户排除。采用内置方案，保持Webhook原始验签，绝不为WS伪造签名或新增免签公网入口。

Config增加receive_mode（webhook/websocket），缺省webhook以兼容旧配置；现有JSONB可存，无业务表迁移。测试站在更新后仅切换此字段，其他配置与凭据保留。后台提供消息接收方式和脱敏连接状态；关闭生图仍可接收识别群，但不自动启用生图。

连接器仅从QQ固定官方HTTPS API取得token与Gateway，检查返回WSS URL、TLS、大小和超时；不接受用户自定义Gateway、不跟随重定向。使用现有websocket库。处理HELLO/IDENTIFY/READY、心跳ACK、RESUME、重连及明确不可重试错误。仅订阅GROUP_AND_C2C_EVENT，仅处理GROUP_AT_MESSAGE_CREATE；忽略C2C和其他事件。

接收和心跳不被发送消息阻塞；有界串行事件队列。业务成功/明确业务拒绝后才推进恢复序列，数据库异常保留旧checkpoint并断线恢复；重复消息继续按原identity+messageID、canonical payload去重。session/token只在内存，不进入API或日志。

运行监督器读取当前QQ配置，使用专用PG session advisory lock维持同库单接收器，连接持有者失联、配置变更、凭据撤销、切换模式时取消旧socket。业务执行事务内重查配置版本与接收模式；识别已有事务版本校验。简易模式不启动接收器。无QQ凭据时等待配置，不使应用启动失败。

## 验证与部署

先运行新增回归失败，再实现。模拟真实WS服务验证握手/心跳/重连/去重/关闭/轮换/拒绝不可信网关；已有签名回归和真实PG资金/发现回归必须继续通过。前端验证模式保存、状态与会话隔离；类型、lint、构建。

生产只做前后只读核对：容器ID/image/start/restarts、配置metadata、nginx hash、证书和health。测试站备份DB和compose，保持2GiB挂载、资源限额、loopback端口、原凭据。更新私有镜像，只重建test app，不重建PG/Redis、不修改nginx/生产目录。通过新管理员只读status确认真实QQ连接READY/心跳；不主动发送QQ/邮件或触发付费生成。真实用户命令仍由用户发送，未完成则明确记录边界。