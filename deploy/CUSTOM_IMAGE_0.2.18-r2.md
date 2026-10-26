# Sub2API 二开 0.2.18-r2 Linux 发布与升级记录

日期：2026-10-27（Asia/Shanghai）。**镜像已发布，双架构运行验证和匿名完整运行层校验通过。用户自行替换服务器镜像，本轮未部署服务器。**

## 固定来源与平台

- 应用源码/OCI revision：`03eb8f3fc4a61729eb6a1525547a2f79b38b71f0`；抽奖v4功能提交，基于r1发布后main `f909291c9`，继承官方v0.2.18与既有QQ开关/图库等二开。
- 发布控制：`35408a76f13de0b9a4c521c167148fc47c6ae39d`；[专用工作流](../.github/workflows/publish-custom-v0218-r2.yml)固定完整应用SHA，要求其最新main/push CI和Security成功，拒绝覆盖最终标签。
- 镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.18-r2`，仅原生Ubuntu构建`linux/amd64`与`linux/arm64`；没有macOS或通用Release构建。
- 最终index digest：`sha256:e64c9d7ee865c4c976c495b611caafb0438b9975ad1b674dc1a0f8dfcf2b26b8`。
- 应用与发布控制已整合主目录main并推送；原11份WIP、4份其他未跟踪文件和293份dist保留，不属于本镜像源码。`.gitignore`合并新文档例外，原用户内容和原LF行尾保留。

## 本版变化

抽奖奖池改为$0.01/$0.02/$0.05/$0.10/$0.20/$0.50/$1，概率45%/30%/18%/5%/1.5%/0.4%/0.1%，均值$0.0305/次，比旧奖池降低97.84%；93%不超过$0.05，最高$1。无空奖，没有累计抽数保底；签到仍采用已有v3小额规则，抽奖资格与福利余额兑换规则不变。

修复旧页面先收到新版抽奖/签到/兑换响应后仍显示旧高奖池的问题；原有历史结果、余额和抽奖次数保留。私有抽奖审计与公开规则升v4，`draw:v2`请求指纹保留历史重放兼容。完整算法、本地验证及独立审查见[抽奖v4记录](../docs/changes/2026-10-27-welfare-lottery-v4.md)。

## 实际验证

| 项目 | 结果 |
|---|---|
| 精确源码CI | [40378772884](https://github.com/Ttt599536561/sub2api/actions/runs/40378772884)，5个job全部成功，含完整unit/integration、前端、lint、部署与发布辅助。 |
| 精确源码安全扫描 | [40378772920](https://github.com/Ttt599536561/sub2api/actions/runs/40378772920)，后端与前端门禁通过；保留既有依赖例外，不宣称零漏洞。 |
| Linux发布 | [40380627800](https://github.com/Ttt599536561/sub2api/actions/runs/40380627800)，gate、两个原生架构build及publish全部成功。 |
| 两架构实际运行 | 版本0.2.18-r2、完整应用SHA、Go1.27.3一致；psql/pg_dump、模型价格资源、setup状态及嵌入前端验证通过。 |
| Artifact | 5份必要ZIP的SHA256与大小匹配GitHub元数据，绑定同run/control/source；独立复核再次计算ZIP并核对解压内容及两架构二进制信息。 |
| 匿名registry | index/runtime manifest/config的哈希、平台、版本、来源及架构artifact绑定通过；24个运行层/90,241,086字节全部SHA256与大小通过；末尾重读标签不变。 |

匿名核验完成于北京时间**2026-10-27 02:53:38**。采用HTTP流式校验，没有向本机Docker引擎拉取镜像；运行验证在GitHub任务执行。没有验证Buildx attestation内容、签名或服务器部署。独立复核验证本地artifact及registry报告/验证器，没有重复下载全部运行层。

| 平台 | Runtime manifest | OCI config |
|---|---|---|
| `linux/amd64` | `sha256:97aef25c44ce7fe01c84093d303783a5d1346d2119f5f1985b7e9530005037ca` | `sha256:44d6ec1c02093584132f340bfdba2b575542185c99ecad5cb2cee585c4f8cc91` |
| `linux/arm64` | `sha256:856cea1790e15564fec06402061348ce0dff123e7b514b2832a62948b6823be9` | `sha256:e8853ff3437d1fb7d95eaa7ef2fc2dc59f56979e771b055f9c3f1d63e6f9e0c5` |

本地功能验证：服务/接口145叶、PG16福利119叶、前端143项，类型/生产构建/专项lint和独立复审通过；本地专项与完整GitHub CI范围分开，不相加。本次发布CI没有修正新业务代码，固定来源始终是上述应用提交。

## 镜像地址

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.18-r2
```

固定内容地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:e64c9d7ee865c4c976c495b611caafb0438b9975ad1b674dc1a0f8dfcf2b26b8
```

## 升级与回退

1. 更新前备份数据库、Compose/配置及原镜像。r1→r2新增`252_welfare_lottery_rules_v4.sql`，启动自动执行，迁移历史307→308；只改福利program版本、默认值与约束，不清空余额/历史/抽数/签到进度，不修改活动开关、开放时间或审计时间。251原字节不变。
2. **先在后台暂停福利，再协调替换所有应用节点，确认新版本健康后恢复。** 旧二进制不会因为数据库版本升v4而采用低奖池，避免新旧发奖节点混跑。已有余额仍可兑换，已完成请求重试仍返回原结果。
3. 回退旧镜像会恢复旧高奖算法，回退前也先暂停福利，优先向前修复。不要清库、倒退历史账本或用旧备份覆盖新增业务数据。
4. 保留现有`QQ_IMAGE_ENCRYPTION_KEY`原值与`QQ_IMAGE_EXTERNAL_HELP=true`，不要替换密钥或覆盖现有Compose；同一个机器人不能同时恢复正式/测试两个收费接收器。r2继承r1的QQ开关和图库，无须重新绑定。
5. 从更早版本升级，另看[r1及之前的升级说明](CUSTOM_IMAGE_0.2.18-r1.md#服务器现状升级和回退)。服务器由用户更新，本轮不连接或更改服务器、QQ配置、CF或智商检测站；不把历史记录中的r7运行状态当成本轮在线核验。

## 本机交接

发布证据保存在主项目`output/publish-v0.2.18-r2`；缓存、必要脚本和WIP保护在`.cache/publish-lottery-v0218-r2-20261027`。应用与控制先后快进整合main，发布记录随后单独提交；未纳入原Codex/CCSwitch等WIP。保留必要证据、共享缓存及历史清理被拒绝目录，本轮未重试删除这些目录，未启动本机Docker/测试PG或修改登录配置。
