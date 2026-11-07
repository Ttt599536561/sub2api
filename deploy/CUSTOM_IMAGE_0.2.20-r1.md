# Sub2API 二开 0.2.20-r1 Linux 镜像

本轮按用户要求在GitHub构建，仅包含linux/amd64与linux/arm64。服务器由用户自行替换镜像，本轮不部署线上。

## 发布准备

目标镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.20-r1`。目前处于发布准备，不能据此认定镜像已经可拉取；最终digest和验证结果将在构建完成后记录。

应用固定源码：`2f398e371e58ed7375f605b0d71f44d515fd6742`，包含官方v0.2.20与六项已复核的合并后修复。源码CI [42371095920](https://github.com/Ttt599536561/sub2api/actions/runs/42371095920)及Security [42371095918](https://github.com/Ttt599536561/sub2api/actions/runs/42371095918)均成功。后续交接文档提交不改变应用代码，主目录原23项WIP不纳入镜像。

[专用发布工作流](../.github/workflows/publish-custom-v0220-r1.yml)只使用两个原生Ubuntu Linux runner；固定完整应用SHA并要求该SHA的main/push CI与Security成功。构建后分别验证OCI来源/版本、二进制Go工具链、pg_dump/psql、价格资源、安装状态与嵌入前端，再发布双架构最终标签。最终标签已存在时拒绝覆盖，没有macOS或Windows构建。

## 从0.2.19-r2升级

- 首次启动自动增加 `249_mcp_oauth.sql`，迁移310→311；保留旧SQL校验值及QQ同号完整文件名。该迁移创建三张MCP OAuth表，不重置用户余额、QQ订单/授权、福利或推广数据。升级前备份数据库和配置。
- 沿用现有QQ加密密钥和接收器设置，保留 `QQ_IMAGE_EXTERNAL_HELP=true`，测试收费接收器继续关闭。独立CF/智商检测站不随本镜像更新。
- MCP默认关闭，不必为此次升级新增 `MCP_PUBLIC_URL`。如之后需要启用，按[管理MCP说明](../docs/mcp.md)配置。
- auth snapshot v26→v28，旧缓存自动重新加载，无须清空Redis或重建API Key；共享Redis的多节点应协调升级，减少新旧快照反复覆盖。
- 上游新增实例请求体缓冲预算默认1GiB，以及读/写/flush单次操作超时120秒，均可配置；不是SSE总时长限制，详见[资源与边缘安全](EDGE_SECURITY.md)。客户端断开后的上游读取计费用量窗口有上限。
- Ultrafast文本Standard×8及媒体预占/计费例外保留。GPT-6.1 Terra的OAuth/setup-token订阅请求扩展长上下文附加费豁免，API Key原价卡保留。

## 本轮验证与状态

工作流YAML、五段bash语法、Linux矩阵/精确来源/不可覆盖检查和独立静态发布复核已通过；尚待GitHub构建、运行和匿名registry验证。原始证据保存在本机 `.cache/publish-v0220-r1-20261107` 与 `output/publish-v0.2.20-r1`。
