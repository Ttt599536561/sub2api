# Sub2API 二开 0.2.17-r7 Linux 发布与升级记录

日期：2026-10-26（Asia/Shanghai）。**镜像已发布，双架构实际运行检查、来源及匿名完整运行层核验通过。** 服务器由用户自行更新，本任务未部署任何实例。

## 固定来源

**上游版本身份更正（2026-10-26）**：官方 [v0.2.18](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.18) 的标签对象`de86961b627e48db75d61fff8c05535d44a00cbe`指向提交`ac1b8f1271be0d674fa6f074403c1410f30a3a81`，该提交已包含在本镜像源码祖先中。因此本镜像已经合入官方0.2.18发布基线。文件名和镜像中的`0.2.17-r6/r7`属于沿用的二开发行命名；此前将该上游提交标为0.2.17的说明有误。上游标签提交里的VERSION文件仍为0.2.17，官方发布脚本会按tag改写构建工作区版本，本仓发布则显式注入二开版本号。已发布镜像地址、二进制和digest保持不变，本次仅纠正文档。

- 应用源码/OCI revision：`70ba0376bda2dba0279eca74aeb36992804d64df`，基于main原`8e5c5d32e`，包含签到v3实现、二审回归与测试规范修正。
- 发布控制提交：`b7c83e429c1d4b3abf0f17269f43ed56a2e6edf4`；[r7专用工作流](../.github/workflows/publish-custom-v0217-r7.yml)固定上述应用SHA，要求其最新main/push CI与安全扫描成功。
- 镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.17-r7`，运行平台仅`linux/amd64`、`linux/arm64`，原生Ubuntu构建；没有macOS或通用Release任务。
- 最终index digest：`sha256:97d789383cdc65d4ff6e758a1b5db4e176ebcbfc23d5fe537450a0bc78b4dccd`。
- 源码和发布配置已整合主目录main并推送；原11份tracked WIP、4份其他untracked及293份dist文件/10,332,156字节保全。.gitignore原用户内容保留并追加两条福利文档例外，WIP不属于镜像。

## 用户可见变化

签到日奖降至$0.01～$0.05；连续第7/15天额外$0.03～$0.08/$0.08～$0.20；第30天额外分档随机$0.20～$1.95，含日奖单次最高$2。首次满勤30天平均约$1.25884，较旧规则降88.56%；整轮不是$2封顶，理论最高$3.73。

已有余额、历史、连续进度和保底计数保留，旧成功请求原样回放；抽奖/兑换算法保持。完整概率、二审与本地验证见[签到v3记录](../docs/changes/2026-10-26-welfare-checkin-v3.md)。

## 实际发布验证

| 项目 | 核验结果 |
| --- | --- |
| 精确源码CI | [40331079734](https://github.com/Ttt599536561/sub2api/actions/runs/40331079734)，5个job全部成功，包含unit/integration、前端、lint与辅助检查。 |
| 精确源码Security Scan | [40331079778](https://github.com/Ttt599536561/sub2api/actions/runs/40331079778)，后端/前端安全门禁成功；不宣称依赖零漏洞。 |
| Linux双架构发布 | [40333254809](https://github.com/Ttt599536561/sub2api/actions/runs/40333254809)，gate、两架构build和publish成功。 |
| 运行及二进制 | 两架构r7版本、完整源码SHA和Go1.27.3相符，x/image v0.45.0；psql/pg_dump、价格资源、setup与嵌入前端验证通过。 |
| 工作流产物 | 5份必要ZIP的SHA256与GitHub元数据一致，绑定同一run/control/source；另有2份Buildx自动记录未作为运行产物核验。 |
| 匿名拉取核验 | index/runtime manifest/config、平台、版本及来源与架构artifact相符；24个运行层、90,211,611字节SHA256/大小全部通过，最后重读标签未变。 |

核验完成时间：北京时间**2026-10-26 21:52:24**。镜像层通过HTTP流式校验，未在本机Docker引擎拉取本次镜像；实际运行检查由GitHub各架构任务完成。合法attestation索引条目允许存在，但未核验其内容或签名。

| 平台 | Runtime manifest | OCI config |
| --- | --- | --- |
| `linux/amd64` | `sha256:7bb90126c4a08d60e262b23e62bfdca276ac802b5900c32812e5276a167b3ba2` | `sha256:d3c0c3ca8054767e0fc3aa914bfd182ea87266491f3e95dfad648d60b0ea39f8` |
| `linux/arm64` | `sha256:ef70b870c18f8d474f64d673ab3b8d59211ceab8f388bdb3c91fdfd0e4e0d512` | `sha256:7f079f4b9950e1c777fb23f664ac5eb16e59e28768b04a0cd2c04e9e6c4d11b1` |

首轮c630的完整测试成功，但lint发现测试QF1003；仅把失败阶段选择改为等价switch，福利专项/Linux目标service lint0 issues及独立复核通过后，重新固定70ba并完成其自己的全部门禁，未绕过检查。

## 镜像地址

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.17-r7
```

固定内容地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:97d789383cdc65d4ff6e758a1b5db4e176ebcbfc23d5fe537450a0bc78b4dccd
```

## 用户升级与回退

1. 用户自行更新服务器；先保留数据库、Compose、配置、密钥备份及原镜像。本任务未操作正式站、QQ测试站、Cloudflare、检测站、真实QQ或收费模型。
2. r6→r7新增251迁移，仅更新福利program版本/default/check，完整迁移306→307；旧余额、流水、活动状态、首次开放时间、连续及保底计数保留。不修改旧SQL/checksum或清空数据。
3. 下一次成功签到立即执行v3，包括升级前已连续6/14/29天的用户；旧奖励不追回，重复请求返回已保存结果。
4. 避免v2/v3发奖节点混跑。回退旧二进制会恢复旧高额算法，应先暂停福利奖励并协调应用版本；不要恢复旧库覆盖升级后新增账务。
5. 若从r4或更早版本升级，还包含[r5 QQ迁移/密钥和资金注意事项](CUSTOM_IMAGE_0.2.17-r5.md)以及[r6 Gemini分组生图开关迁移](CUSTOM_IMAGE_0.2.17-r6.md#升级与回退)。升级后核查health、版本、迁移、余额及启用模块。

## 本机证据

原始核验产物位于`output/publish-v0.2.17-r7`，过程日志、WIP保护和验证脚本位于`.cache/publish-welfare-r7-20261026`。复用已有依赖缓存，没有新建本机Docker构建或测试容器；此前被审批拒绝的福利测试临时目录保持原样，未重试清理。
