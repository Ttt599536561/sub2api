# Sub2API 二开0.2.17-r4发布与升级记录

日期：2026-10-23（Asia/Shanghai）。镜像已发布并通过双原生架构运行检查及匿名完整运行层核验。用户要求本轮不部署生产；生产仍为0.2.17-r3。上一版见[0.2.17-r3](CUSTOM_IMAGE_0.2.17-r3.md)。

## 发布身份

| 项目 | 已核验结果 |
| --- | --- |
| 镜像标签 | `ghcr.io/ttt599536561/sub2api-custom:0.2.17-r4` |
| 卡片功能来源 | `6f90164d5b55584f8266764d2d26f5c6a9bd1a9d` |
| 镜像源码 / OCI revision | `76ff7c88a0e792b39936346db9c9707930eba0ef`，含用户批准的两项最小安全补丁 |
| 工作流控制提交 | `867f638f491c97938a84df657c969bc2144f2e07`，不改变被构建的应用源码 |
| 精确源码CI | [39831429931](https://github.com/Ttt599536561/sub2api/actions/runs/39831429931)，main/push，attempt1，success |
| 精确源码安全扫描 | [39831429976](https://github.com/Ttt599536561/sub2api/actions/runs/39831429976)，main/push，attempt1，success |
| 发布运行 | [39833407670](https://github.com/Ttt599536561/sub2api/actions/runs/39833407670)，attempt1，gate/amd64/arm64/publish全部success |
| 多架构index digest | `sha256:fb4c5f40831a5ce4cb6491d1206256f8f5ef9a2bd13cbf126a292ab45eb21875` |
| 支持平台 | `linux/amd64`、`linux/arm64`，由对应原生runner构建和运行核验 |
| 实际二进制Go版本 | 两架构均为`go1.27.3`，从已发布digest镜像提取二进制后用go version -m核验 |
| 生产状态 | 本轮未部署；保持0.2.17-r3 |

推荐固定内容地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:fb4c5f40831a5ce4cb6491d1206256f8f5ef9a2bd13cbf126a292ab45eb21875
```

专用[发布工作流](../.github/workflows/publish-custom-v0217-r4.yml)固定上述源码，要求该SHA最新main/push CI及Security Scan成功；最终标签在构建前和发布前均检查不存在。本轮没有覆盖旧标签，没有构建macOS镜像或运行Apple容器测试。

## 变化与安全修复

- 用户侧V2默认按厂商与用户倍率展示卡片，保留卡片/详细切换、现有详细筛选/趋势/明细；三项指标、历史时间条与健康判定沿用后台数据。见[卡片实施记录](../docs/changes/2026-10-23-monitor-cards.md)。
- 原卡片提交的安全扫描发现Go1.27.2标准库证书验证漏洞及follow-redirects1.16.0两项high告警，用户批准最小修复并改用新SHA发布。Go统一1.27.3，follow-redirects仅升至1.16.1，其他包及业务Go源码不变，不新增豁免。见[安全修复记录](../docs/changes/2026-10-23-r4-security-patches.md)。
- r3→r4没有新增SQL迁移、必需环境变量或推广入口配置变化；保留迁移247、现有数据卷、环境和代理配置。旧部署若早于r3，仍须按对应历史升级文档处理已有迁移。

## 实际验证

| 范围 | 结果 |
| --- | --- |
| 新SHA GitHub后端unit | 58个测试包通过，0个失败包 |
| 新SHA GitHub后端integration | 52个测试包通过，0个失败包 |
| 新SHA GitHub前端 | 类型检查、30文件581项关键测试通过 |
| 新SHA Go lint、部署脚本、发布辅助 | 对应job全部成功，Go lint为0 issues |
| 新SHA安全门禁 | 后端扫描0个可达漏洞、0个已导入包漏洞，另有7个未调用模块漏洞提示；前端审计检查器通过，两项follow告警消失，现有xlsx例外未扩大 |
| 本机安全补丁前端 | pnpm9冻结安装、全量433文件3718项、类型/lint/生产构建通过；复用981依赖包，仅新下载1包 |
| 两架构运行检查 | 版本r4、源码revision、平台、Go1.27.3、pg_dump/psql、定价资源、隔离/setup/status与前端assets通过 |
| 工作流产物核验 | 5份所需ZIP的SHA与GitHub artifact元数据一致，run/control绑定一致；两份版本/build-info均匹配 |
| 匿名registry核验 | index/runtime manifest/config摘要与大小、架构、revision/version/source、两个架构工作流digest绑定一致；全部24个去重运行层、89,525,611字节的SHA256及大小通过；末尾重读标签仍为同digest |

匿名核验时间：北京时间 **2026-10-23 02:49:37**。没有向本机Docker引擎拉取镜像或生成Docker构建缓存；运行层经HTTP流式读取并核验。未验证attestation内容、签名或SLSA来源证明，image-provenance产物只包含版本和Go构建信息。Go测试是非verbose包级汇总，不能推算叶用例/跳过数；GitHub关键前端581项与本机全量3718项分别记录。

| 平台 | Runtime manifest digest | OCI config digest |
| --- | --- | --- |
| `linux/amd64` | `sha256:6bc54b4d359090f3e346be4f5743784094d1c734f455a454003f797948e9cc8d` | `sha256:b5998bf2ed88d28d85178d86a0f9ad0b28bef18429cabc4a451aabd6d84dcfc0` |
| `linux/arm64` | `sha256:a5b78b582f7a249ff3fbdca3c0f19651a4e16d993d30e58a75206bead27927c4` | `sha256:50b73b4acf82520c2f3db6d3ca8862c63872d892a8171b96f809a1bc7647af8a` |

- `amd64` 构建产物：`sha256:81441a63c0c6dcb6a51ac90bc96cfe75ab99e810e1ef289a1ba57aecf0a55113`。
- `arm64` 构建产物：`sha256:737c2fcf13ad9c14aca44820222d00b021ef8c0beb50099180af9a4f9b3e9d28`。

本机证据在主项目`output/publish-v0.2.17-r4`：registry-verification.json、provenance-verification.json、CI/publish job与日志、原始artifact ZIP和解包产物；补丁和工作流检查在`.cache/publish-monitor-r4-20261023`。关键身份与结果已归档本页，本机产物不随Git发布。

## 从现有r3升级（由用户执行，本轮未执行）

1. 在当前部署目录先备份数据库、`.env`、Compose和持久配置，记录当前r3镜像。沿用现有Compose文件及`-f`参数，不替换整份配置、不删除数据卷。
2. 只将应用服务`sub2api`的`image`改为上述固定内容地址（也可使用`ghcr.io/ttt599536561/sub2api-custom:0.2.17-r4`标签）。保留端口、网络、数据卷、PG/Redis和推广入口代理配置。
3. 在原部署目录逐步执行，命令中的Compose选项沿用现有部署：

```sh
docker compose config --quiet
docker compose pull sub2api
docker compose up -d --no-deps sub2api
docker compose ps sub2api
docker compose logs --tail=100 sub2api
```

4. 等应用容器达到healthy后核对版本及安装状态：

```sh
docker compose exec -T sub2api /app/sub2api -version
docker compose exec -T sub2api wget -q -O - http://127.0.0.1:8080/health
docker compose exec -T sub2api wget -q -O - http://127.0.0.1:8080/setup/status
```

版本应为`0.2.17-r4`，commit应为`76ff7c88a0e792b39936346db9c9707930eba0ef`；health应返回status ok。**已安装生产的needs_setup应为false**，不要照搬发布隔离环境的true断言。

5. 经现有HTTPS入口检查登录、`/monitor`卡片/详细切换、刷新及推广入口。后台需启用渠道监控且选择V2；V1模式不受本次卡片改动影响。刷新浏览器以加载新前端资源。health只表示HTTP服务存活，不代替这些业务核验。

## 回退与生产边界

本轮没有生产操作。若升级后需要回退，在保留现有数据库和配置的前提下将同一应用服务换回先前r3固定镜像并只重建该服务：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:52736abda13f53c41a051c2d11a96ca07c8ed5d6c64f3f87731c2a12202e6337
```

r3→r4没有新增schema回退步骤；回到r3也会退回本次两项安全补丁。不要执行docker compose down -v，不重置已有推广域名、票据或数据库状态。

维护入口：[交接](../docs/HANDOFF.md)、[更新日志](../CHANGELOG.md)。
