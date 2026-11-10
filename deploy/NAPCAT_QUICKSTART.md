# 0.2.20-r2 首次启用邀请月卡

已发布的 Sub2 镜像：

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.20-r2
```

首次部署包含“更新 Sub2 镜像”和“新增 NapCat 容器”两部分。原官方生图/智商机器人继续保留；NapCat 使用另一个普通福利 QQ 账号。

## 1. 保留原部署

先备份现有数据库、Compose、环境文件与加密密钥。将本包放入**当前正在使用的 Compose 文件所在目录**，沿用原项目名、数据库/Redis服务和数据卷。

在原 Sub2 服务中将 `image` 改为上面的地址；若原文件通过环境变量设置镜像，就更新该变量。不要替换整份主 Compose，不要运行 `down -v`。

## 2. 初始化一次

服务器需 Python 3。把示例 QQ 号换成准备扫码的普通福利号，把域名换成实际 Sub2 域名：

```bash
python3 napcat/init.py --qq 123456789 --public-url https://api.tangguo.xin
```

此步骤生成私有凭据和登录持久目录，默认关闭发奖。重复运行会拒绝覆盖；不要删除已有密钥或QQ会话来重跑。原 `QQ_IMAGE_ENCRYPTION_KEY` 保持，新模块使用独立 `QQ_INVITE_ENCRYPTION_KEY`。

## 3. 启动

下例主文件名为 `docker-compose.local.yml`，请替换成你原来的文件名；原来用了 `-p 项目名` 时，下面每条命令都保留该参数。

```bash
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml config --quiet
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml pull sub2api napcat
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml up -d --no-deps sub2api napcat
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml ps sub2api napcat
```

NapCat 的 WebUI/OneBot 端口不公开到宿主机，Sub2 后端通过容器网络访问。既有数据库与Redis不会被上述 `--no-deps` 命令重建。

## 4. 在后台完成设置

1. 打开“管理后台 → 福利中心 → QQ邀请奖励 → 连接与扫码登录”。
2. 获取二维码，用指定的福利 QQ 号扫码并确认，再刷新状态，确认显示该账号在线。
3. 将该福利号加入活动群，配置活动群、月卡分组、起止时间与自定义文案。
4. 同步当前成员基线；添加或生成30天月卡码，再开启活动和奖励接收。
5. 用小范围真实邀请测试欢迎、等级、邀请归属和私信，再正式开展活动。

QQ账号等级必须大于10。无法确认真实邀请者的普通审批事件进入待审核，不会把审批管理员当邀请者。私信受限时群内提示补领；若QQ要求好友或安全验证，先在QQ端处理，再私信“领取奖励”。后台可重登和明确重发原码。

同分组月卡兑换后增加时间，不同分组分别持有订阅。活动创建后奖励分组固定，更换种类请新建活动。

只有先更新网站、暂不开活动时，可以先只更新 Sub2 镜像；邀请码池和自动发奖默认不会自行开启。

完整维护、备份和回退说明见 [NAPCAT_SETUP.md](NAPCAT_SETUP.md)。发布证据见[0.2.20-r2记录](https://github.com/Ttt599536561/sub2api/blob/main/deploy/CUSTOM_IMAGE_0.2.20-r2.md)。
