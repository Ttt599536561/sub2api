# Sub2API 二开 0.2.18-r1 Linux 发布与升级记录

日期：2026-10-27（Asia/Shanghai）。**镜像已发布，双架构运行检查及匿名完整运行层校验通过。新镜像由用户自行更新服务器。** 正式QQ配置接管已另行完成并由用户确认绑定成功，线上应用仍为此前的r7。

## 固定来源与平台

- 应用源码/OCI revision：`b048bc329a06b227ced2324af6001fc1960e44c8`；功能实现4ae80f8ad，后续只修正管理设置接口测试契约。基于740c0c582，继承官方v0.2.18对应ac1b8f1、既有二开与r7签到v3；未额外合并上游未发布main。
- 发布控制：`c612676d61a5167b68808acd8a0d2d11e8a476fb`；[专用工作流](../.github/workflows/publish-custom-v0218-r1.yml)固定应用SHA，要求其自己的最新main/push CI和Security Scan成功，拒绝覆盖最终标签。
- 镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.18-r1`，仅原生Ubuntu构建`linux/amd64`与`linux/arm64`；没有macOS或通用Release构建。
- 最终index digest：`sha256:899cdd8b73f64853c7421ea9f1a441fb2d544c4f47f26488b59f0db9c73f2de9`。
- 源码和发布配置已合入主目录main并推送；原11份WIP、4份其他untracked、293份原dist文件保全，不在镜像源码内。

## 本版功能

管理员“系统设置→功能开关”可分别暂停新QQ绑定和新QQ生图；后端覆盖绑定三阶段、WebSocket/Webhook和新单准入，保留旧事件去重、历史订单、解绑、旧单处理及资金操作。缺失字段默认true兼容原行为，原技术配置/白名单/证明仍生效；旧PUT省略字段不覆盖并发关闭。

用户订单升级为桌面3列/平板2列/手机1列的作品网格与大图详情，可搜索筛选最近100条、下载和复制提示词/订单号；仅有效且已结算图片提供QQ重发指令。私图保持鉴权、最多2并发与12张/64MiB缓存，过期/会话变化撤销；晚到免费图可领取，生成/资金/投递状态独立。绑定404显示站点匹配提示，观察到暂停时统一清核对码。

完整设计、红绿回归、审查修复与真实接管见[本轮记录](../docs/changes/2026-10-27-qq-controls-gallery.md)。基础VERSION同步0.2.18；历史0.2.17-r7已包含官方0.2.18基线，本次不是重复合并同一上游提交。

## 实际验证

| 项目 | 核验结果 |
| --- | --- |
| 精确源码CI | [40364270141](https://github.com/Ttt599536561/sub2api/actions/runs/40364270141)，5个job全部成功，包含unit、integration、前端、lint及辅助。 |
| 精确源码安全扫描 | [40364269888](https://github.com/Ttt599536561/sub2api/actions/runs/40364269888)，后端/前端门禁成功；保留既有依赖例外，不宣称零漏洞。 |
| Linux发布 | [40366486180](https://github.com/Ttt599536561/sub2api/actions/runs/40366486180)，gate、两架构build、publish全成功。 |
| 实际运行与二进制 | 两架构0.2.18-r1/完整源码SHA/Go1.27.3匹配，x/image v0.45.0；psql/pg_dump、价格资源、setup状态和嵌入前端验证通过。 |
| Artifact | 5份必要ZIP SHA256匹配GitHub元数据，绑定同run/control/source；另外2份Buildx记录未作为运行产物核验。 |
| 匿名registry | index/runtime manifest/config/平台/版本/来源和架构artifact绑定通过；24个运行层/90,240,959字节SHA256与大小全部通过，末尾重读标签未变。 |

匿名核验完成：北京时间**2026-10-27 01:21:39**。采用HTTP流式校验，没有向本机Docker引擎拉取镜像。运行验证由GitHub任务执行；没有验证attestation内容或签名。

| 平台 | Runtime manifest | OCI config |
| --- | --- | --- |
| `linux/amd64` | `sha256:81162c271fe90c23e8b911c3b288958587f1d21be77db09819cbf9fbe2395c4f` | `sha256:1594182ff8ebefe52ed5191a4dfe8388837cb9a774b312d591a3f71bd563fa2c` |
| `linux/arm64` | `sha256:31c09d0e61e1c49212d3acd1217f0ed02a60d2127c4f6c88f6dba541ccb48d4a` | `sha256:496d56260eee5d15ce81f802f41d782a7ec1ea08549964616f5865b366e06dc9` |

本地四包3,456 PASS事件/3,149叶、完整QQ真实PG514 PASS事件/474叶、SSR129 PASS事件/103叶，均0失败0skip，范围不相加；五包vet和Linux CI模式lint0 issues。前端完整445文件4,010项后增加一项邮箱验证暂停清码回归，相关67项及最终类型/lint/build通过，所有新增关键用例已纳入最终CI。浏览器真实组件/假API验证明暗与三断点，所查console无warn/error。首轮4ae CI仅因两份管理设置JSON期望遗漏两个新字段失败，修正4行保留JSONEq后，b048已重新通过全部精确门禁。

## 镜像地址

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.18-r1
```

固定内容地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:899cdd8b73f64853c7421ea9f1a441fb2d544c4f47f26488b59f0db9c73f2de9
```

## 服务器现状、升级和回退

1. 正式站仍运行原0.2.17-r7，仅已完成QQ配置接管：独立加密密钥、QQ_IMAGE_EXTERNAL_HELP=true、正式配置1及api.tangguo.xin绑定域名；测试配置8停收。用户已确认正式链接绑定成功。新开关/图库需用户自行替换为本镜像后生效，不需要重新绑定。
2. **保留现有QQ_IMAGE_ENCRYPTION_KEY原值和QQ_IMAGE_EXTERNAL_HELP=true。不要重新生成/替换密钥**，否则既有Bot/API密文不能解密。更新前备份数据库/Compose/配置及原镜像；不要下载新Compose覆盖现有秘密配置。
3. r7→本版没有新增SQL schema迁移。两个settings键缺失默认true，不会绕过原QQ技术启用/白名单/邮箱证明；管理员可分别关闭新绑定/新生图，历史管理仍可用。
4. 业务开关关闭不是关闭接收器；测试站已独立停收，不能与正式站同时恢复处理同一收费指令。CF菜单/智商及测试published模板源不改，生产使用自身默认文案；未迁移测试用户、余额、绑定、订单或模板历史。
5. 回退旧镜像会失去本版开关执行与新UI，需要时先用旧技术配置停新业务；接收器回退须先停正式并等待在途/冻结/连接释放，再恢复测试。禁止倒灌旧库覆盖新账务。旧QQ密文没有永久撤销，私有备份在服务器/root/sub2api-deploy/qq-cutover-backup-20261026。
6. 从更早版本升级，另看[r7及之前的迁移说明](CUSTOM_IMAGE_0.2.17-r7.md#用户升级与回退)。

## 本机记录

发布原始证据在`output/publish-v0.2.18-r1`，发布缓存/WIP保护在`.cache/publish-qq-v0218-r1-20261027`，功能测试/接管阶段日志在`.cache/qq-controls-gallery-20261026`。只保留必要证据和共享依赖；本机测试PG与QA预览均已停止，未重试删除先前被审批拒绝的旧目录。视觉验收改用受控前台本地会话后完成，没有执行被拒绝的后台启动/停止脚本。
