# 邀请月卡接入与升级

Sub2 镜像版本和生产部署状态以[当前代码和发布状态](../docs/HANDOFF.md#当前代码和发布状态)及对应发布记录为准。部署时将下面的版本占位符替换为发布记录中的应用版本：

```text
ghcr.io/ttt599536561/sub2api-custom:<发布记录中的版本>
```

2026-11-11 03:16（北京时间）曾完成r3生产应用更新并核验网站健康和原QQ在线，见[历史发布与部署记录](CUSTOM_IMAGE_0.2.20-r3.md)。本文的无QQ等级门槛说明对应当前源码，旧版镜像不因此自动改变规则。取消等级限制不更换 NapCat 镜像，已有登录无需重新扫码。

## 已接入 NapCat：只升级 Sub2

先备份数据库、当前 Compose 和环境文件。沿用原 Compose 项目名、主文件及生产 `docker-compose.napcat.yml`，把 Sub2 的镜像地址更新为上面的版本。保留数据库/Redis、现有网络设置、NapCat 会话和全部密钥；已有接入不再运行初始化脚本。

下面以 `docker-compose.local.yml` 为主文件；原来使用了 `-p 项目名` 时，每条命令继续带同一参数：

```bash
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml config --quiet
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml pull sub2api
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml up -d --no-deps sub2api
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml ps sub2api napcat
```

更新后检查网站健康，在后台刷新原QQ在线状态，再刷新活动页面。已有活动、库存和开关保持原状态；本次应用升级不会补充库存或替管理员开启活动。

## 首次接入

首次部署包含“更新 Sub2 镜像”和“新增 NapCat 容器”两部分，按下面步骤执行。原官方生图/智商机器人继续保留；NapCat 使用另一个普通福利 QQ 账号。

### 1. 保留原部署

先备份现有数据库、Compose、环境文件与加密密钥。将本包放入**当前正在使用的 Compose 文件所在目录**，沿用原项目名、数据库/Redis服务和数据卷。

在原 Sub2 服务中将 `image` 改为上面的地址；若原文件通过环境变量设置镜像，就更新该变量。不要替换整份主 Compose，不要运行 `down -v`。

### 2. 初始化一次

服务器需 Python 3。把示例 QQ 号换成准备扫码的普通福利号，把域名换成实际 Sub2 域名：

```bash
python3 napcat/init.py --qq 123456789 --public-url https://api.tangguo.xin
```

此步骤生成私有凭据和登录持久目录，默认关闭发奖。重复运行会拒绝覆盖；不要删除已有密钥或QQ会话来重跑。原 `QQ_IMAGE_ENCRYPTION_KEY` 保持，新模块使用独立 `QQ_INVITE_ENCRYPTION_KEY`。

### 3. 启动

下例主文件名为 `docker-compose.local.yml`，请替换成你原来的文件名；原来用了 `-p 项目名` 时，下面每条命令都保留该参数。

```bash
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml config --quiet
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml pull sub2api napcat
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml up -d --no-deps sub2api napcat
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml ps sub2api napcat
```

NapCat 的 WebUI/OneBot 端口不公开到宿主机，Sub2 后端通过容器网络访问。既有数据库与Redis不会被上述 `--no-deps` 命令重建。

### 4. 在后台完成设置

1. 打开“管理后台 → 福利管理 → QQ邀请奖励 → 连接与扫码登录”（`/admin/welfare`）。
2. 获取二维码，用指定的福利 QQ 号扫码并确认，再刷新状态，确认显示该账号在线。
3. 将该福利号加入活动群，在“活动与奖励”创建活动，确认月卡订阅分组、群号、起止时间与自定义文案并保存。
4. 在“月卡库存”手动生成30天月卡并入池，或预检后导入同分组的未使用30天码；每批最多500张，没有自动补库存开关。入池前先确认订阅分组。
5. 在“活动设置”同步现有成员基线；库存就绪后“启用活动”，再到“连接与扫码登录”打开“启用邀请奖励接收”并保存。这两个开关独立于签到、抽奖开关。
6. 活动配置完成后，由管理员安排小范围真实邀请，验收欢迎、邀请归属、活动内去重和私信，再正式开展活动；不再查询或验收QQ等级。

邀请奖励不设QQ等级门槛，也不查询等级；低等级或等级未知均不会因此被拒绝，机器人自身等级同样不受限制。指定机器人仍须在线且匹配配置。同一被邀请人在整场活动中仅奖励一次，邀请者邀请不同合格新人可多次获奖。无法确认真实邀请者的普通审批事件进入待审核，不会把审批管理员当邀请者。私信受限时群内提示补领；若QQ要求好友或安全验证，先在QQ端处理，再私信“领取奖励”。后台可重登和明确重发原码。

旧“等级查询待处理”记录会在邀请奖励接收启用且到达重试时间后按新规则自动继续；旧“等级规则未通过”记录保留历史结果，不会批量补发，可逐条“重查资格”。新记录不采集等级，后台及文案变量 `{level}` 显示“未采集”；历史等级数字保留。请检查并移除活动自定义文案中的旧等级门槛，已有记录快照和待投递消息不会随活动文案修改而重写。

同分组月卡兑换后增加时间，不同分组分别持有订阅。

## 活动修改规则

仅当前和本次保存均停用、历史上从未启用、无任何月卡库存且无邀请或入群历史的草稿可修改订阅分组。曾启用后暂停，或已有库存（包括已用、已分配、过期码）、邀请历史的活动如需换组，请新建活动。只改订阅分组会保留现有QQ成员基线；改变QQ群范围后须重新同步基线。

保存版本落后时，需重新加载最新版本再编辑；若提示当前状态不允许，还应核对活动状态及群和时间范围是否与其他已启用活动冲突。“订阅分组锁定”表示活动不符合草稿换组条件，刷新不会解除。保存成功后页面直接接纳返回的新版本；保存失败保留输入，不自动重试。

首次接入时邀请奖励默认关闭；只更新网站、暂不开活动时，可以先只更新 Sub2 镜像。完整维护、备份和回退说明见 [NAPCAT_SETUP.md](NAPCAT_SETUP.md)，首次扫码验收见[部署记录](../docs/changes/2026-11-11-napcat-deployment.md)。
