# AI 启动入口：先定位项目，再接手任务

用途：跨对话恢复项目身份与边界。核对日期：2026-10-26，Asia/Shanghai。本文和 [PROJECT_INDEX.json](PROJECT_INDEX.json) 是稳定路由索引；版本、余额、在线状态不是本文的实时事实。

## 最短加载顺序

1. 读 `E:/sub2二次开发项目/docs/PROJECT_INDEX.json`，由用户用词、明确 URL/文件路径及本次上下文选择 repository/service/environment；当前 CWD 和侧栏名称只作线索。
2. 到选中的 canonical root，先读当地 `AGENTS.override.md`（若有）或 `AGENTS.md`，再读 `docs/AI_CONTEXT.md`；需要跨服务时只加载相关服务的卡片与入口。
3. 按该仓要求读 HANDOFF 的接手摘要和最新 CHANGELOG；只继续读与任务相关的发布/变更记录。日期旧的“当前”“待完成”属于当时状态，不能覆盖新证据。
4. 每次 PowerShell 调用在同一进程加载目标仓的绝对 `tools/project-env.ps1`；Sub2 工作树使用主目录入口，检测站始终使用自己的入口。然后检查实际工作目录、Git HEAD/分支/dirty 状态。
5. 在开始改动前说明本次选择的仓库、服务和环境。只有仍有实质歧义时才问用户；不要让用户重复提供索引已经包含的信息。

## 身份模型

- **两个源码仓库**：`sub2api` 与 `ai-check`。
- **QQ生图是Sub2内的业务模块**：`qq-image`，不是第三个源码仓库；已合并主目录，旧 QQ 工作树不再是默认入口。
- **一个用户可见QQ机器人，两个独立接收服务**：Sub2 的生图接收器，与检测仓的 `oneapi-observer-qq` 菜单/智商接收器。单个连接健康不能证明整条机器人链路健康。
- **六个定位对象**：Sub2应用、QQ生图模块、检测站Sites、检测HTML生成Worker、检测定时Worker、QQ菜单/智商Worker。模块与部署单元不是一一对应。
- **测试环境不是检测项目**：`qq-test.tangguo.xin` 是Sub2独立验收实例；智商检测站是独立产品，域名含sub2也不属于Sub2部署。

## 路由优先级与防混淆

明确路径/域名和用户纠正优先于别名。以下是现有联合机器人模式：

| 用户需求/症状 | 首查位置 | 必须分开的边界 |
| --- | --- | --- |
| 裸 @、/菜单、/帮助 没有回复 | ai-check/cloudflare-qq | 不先改余额或Sub2邮箱 |
| /智商 查询、截图、分组错误 | cloudflare-qq + ai-site | 截图只读已有作品，不是新生图 |
| /绑定、确认绑定、邮件验证码 | sub2api 的 qqimage/auth/email/UI | 不用检测站管理员密码或CF管理令牌 |
| /生图、余额、价格、订单、重发 | sub2api 的 qqimage | 不是 oneapi-observer-generation |
| 指令归属/重复回复/紧邻提示词 | 两侧命令解析 | CF静默让出，不是HTTP代理到Go；共同保留边界 |
| 回复文案编辑/同步 | Sub2模板后台 + CF六键读取 | 内容编辑、默认模板和同步协议是不同任务 |
| 检测站首页、正常率、颜色、鹈鹕 | ai-site | Sub2“站点监控”卡片是另一功能，不按“监控”一词误路由 |
| 自动检测30分钟/夜间暂停 | ai-scheduler + ai-site | 不是QQ每分钟心跳巡检 |
| QQ重连/心跳告警 | 先确认是哪套连接；CF故障查qq-menu-iq | Go网关与CF网关不共用连接状态 |
| GitHub构建Sub2镜像 | Sub2发布/CI | 发布镜像不等于部署生产，也不会部署CF/Sites |

裸称“生图”“机器人”“后台”“正式站”而上下文不足时，先用此表缩小到两个候选，再问一个定位问题。`image-2`、`iamge-2重构`、即梦等侧栏项目未纳入本次索引，不擅自归入QQ生图。

## 开工记录与事实等级

内部先确定：`repository_id / service_ids / environment / workdir / env_script / source_state / evidence_date`。写入或部署前核对目标身份；此前授权按原范围继续有效，路由文档不是要求重复确认的审批流程。

`身份/职责` → 本索引与卡片；`实际实现` → 当前源码/配置；`最后已知发布` → 对应服务带日期交接；`现在线上状态` → 必要时只读核实。主目录HEAD、工作树HEAD、构建源码、发布镜像、运行实例分别记录，不互相代替。不要将历史“未合并/0余额/未配置SMTP”当成现在的结论。

## 维护契约

- 主索引唯一维护于 Sub2 的 `docs/PROJECT_INDEX.json`；全局和两仓 AGENTS 只放短指针，避免多份完整索引漂移。
- 目录、别名、服务拆分、域名、调用关系变更：更新主索引及对应仓 `docs/AI_CONTEXT.md`，同步当地HANDOFF/CHANGELOG。普通版本变更只更新服务交接，不把版本号写入所有启动入口。
- 本机全局入口为 `C:/Users/Administrator/.codex/AGENTS.md`；全局文件及本仓AGENTS属于现有本机文档（仍按原规则不入Git），机器索引/卡片/路由规则纳入仓库文档；Sub2还存在优先加载的本机 `AGENTS.override.md`，必须与仓库入口保持相同定位规则。不同机器/账号/CODEX_HOME需重新安装此短指针；不得保证跨机器自动生效。
- 全局入口仅对这组项目生效，不覆盖其他项目约定；不在索引放密码、Token、SMTP凭据、用户邮箱或完整私有配置。
- 索引缺失时按全局简表和仓库卡片找源码，不创建同名项目、不复制仓库、不重置工作树。所有新工作区、缓存与备份留在E盘，保留既有修改。

入口选择依据：[官方 AGENTS.md 加载说明](https://learn.chatgpt.com/docs/agent-configuration/agents-md)。本轮仅整理文档和入口，不改变程序、数据、部署、侧栏项目或登录配置。
