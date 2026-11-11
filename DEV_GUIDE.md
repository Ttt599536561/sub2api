# sub2api 项目开发指南

> 本文档记录开发环境与维护规范。AI接手先读 [项目定位入口](docs/AI_START_HERE.md) 和 [Sub2上下文卡](docs/AI_CONTEXT.md)，明确仓库/服务/环境后再读本文。

## 更新记录与交接规范

接手项目先阅读 [更新日志](CHANGELOG.md) 和 [当前交接](docs/HANDOFF.md)。已发布镜像的地址、固定源码与升级说明从交接页进入对应发布记录；不要把某一历史版本作为长期入口。旧报告的版本与验证数字仅代表其历史阶段。

- 每次提交项目自身的代码更新，包括功能、修复、兼容、配置、迁移及 CI/发布变更，都要同步维护 `CHANGELOG.md`。上游批量合并以一次升级汇总，不逐条抄录所有上游提交；后续本地兼容修正仍分别记录。
- 记录日期（Asia/Shanghai）、稳定的变更名称或编号、改了什么、原因与影响、实际验证结果，以及部署/迁移注意事项。普通改动几行即可；重大升级在 `docs/changes/YYYY-MM-DD-主题.md` 保存详情并从日志链接。
- 提交前使用稳定变更标识与实际提交主题关联；同一提交不能预知自身 SHA，不写猜测值。提交后可在下一次记录整理补充已存在的 SHA，或通过 `git log -- CHANGELOG.md` 追溯。已发布版本必须写明固定源码提交。纯文档更正直接维护原条目，避免重复生成文档自引用记录。
- 发布、部署状态、二开规则、待办或未提交工作变化时，同步更新 `docs/HANDOFF.md`；清楚区分工作区、已提交源码、已发布镜像和线上部署。发布记录保存在 `deploy/`，不要把“镜像发布成功”写成“生产已升级”。
- 验证只记录实际执行结果：注明命令/范围、通过或失败/跳过、对应源码与环境。未执行的检查标明原因；不得用旧阶段测试数字宣称当前源码通过。本机工作区包含未提交改动时，说明它与发布源码的区别。
- `output/` 与 `.cache/` 是本机产物和原始证据目录，不随 Git 交接。关键结论、提交、行为变化与验证摘要必须归档到受跟踪文档；引用本机证据可作附注，不能作为唯一的交接说明。
- 提交前检查文档链接、提交引用和 `git diff --check`；用 `git check-ignore` 与 `git ls-files` 确认新记录已纳入 Git。新增目录需精确配置忽略例外，避免把缓存和构建产物一起纳入。
- 更新记录与对应代码放在同一提交，保留已有用户修改。若文档是事后补录，明确历史日期和代码基线，单独提交；不为整理文档重新运行无关应用测试。

## 一、项目基本信息

| 项目 | 说明 |
|------|------|
| **上游仓库** | Wei-Shaw/sub2api |
| **Fork 仓库** | Ttt599536561/sub2api |
| **技术栈** | Go 后端 (Ent ORM + Gin) + Vue3 前端 (pnpm) |
| **数据库** | PostgreSQL 16 + Redis |
| **包管理** | 后端: go modules, 前端: **pnpm**（不是 npm） |

## 二、本地环境配置

### Windows：项目缓存和临时文件放 E 盘

本机主项目位于 `E:\sub2二次开发项目`。每个新 PowerShell 会话或自动化工具调用，先载入项目环境；环境变量只影响当前进程及其子进程，不修改系统和用户全局设置：

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目\backend'
go test -tags=unit ./...

Set-Location 'E:\sub2二次开发项目\frontend'
corepack pnpm@9.15.9 install --frozen-lockfile
corepack pnpm@9.15.9 test:run
```

也可用包装器运行单条命令（需要 PowerShell 7.3+，保留参数及命令退出码）：

```powershell
pwsh -NoProfile -File 'E:\sub2二次开发项目\tools\with-project-env.ps1' corepack pnpm@9.15.9 --dir 'E:\sub2二次开发项目\frontend' test:run
```

脚本把 Go 编译/模块/工具缓存、golangci-lint、npm/pnpm 缓存和 `TEMP`、`TMP`、`TMPDIR`、`GOTMPDIR` 统一指向主项目 `.cache`；也设置 pip、uv、Playwright、Node 编译缓存和 Python 字节码缓存的路径，不安装这些工具。`TEMP/TMP` 同时覆盖 Go 测试 `t.TempDir()` 等运行时临时文件。测试日志和临时脚本也应放 `.cache`，需交付的文件放项目 `output`。前端构建仍输出到项目 `backend/internal/web/dist`。

脚本同时配置 `npm_config_*` 与 `PNPM_CONFIG_STORE_DIR`、`PNPM_CONFIG_CACHE_DIR`、`PNPM_CONFIG_STATE_DIR`，兼容不同 pnpm 版本的缓存入口。`COREPACK_HOME` 指向 `.cache/corepack`，供 Corepack 保存 pnpm 等包管理器。历史本机依赖曾由 pnpm 10.12.3 建立，默认 shim 也可能与 CI 不同；切换包管理器版本和迁移缓存是两个独立操作。当前仓库的版本基线见下方“开发工具”，不要因本机 shim 版本而改动锁文件。

所有新工作树放 `E:\sub2二次开发项目\.worktrees`，从工作树运行时仍使用上面的主项目绝对路径入口，复用同一份缓存。不要在 C 盘新建项目临时目录或工作树。`AGENTS.md` 和 `AGENTS.override.md` 保存了本机的后续执行约定。

已有的 C 盘工作树仅作只读检查，并调用主项目入口。安装、构建、测试、代码生成必须先转到 E 盘工作树执行；`node_modules`、`.vite`、覆盖率和构建产物仍可能按工作目录写入，环境变量不能把这些输出自动迁移。新工作树使用 E 盘明确路径，不使用默认落在 C 盘的托管工作树位置。本机 `AGENTS.override.md` 和 `.worktrees` 已加入 `.git/info/exclude`，不修改仓库共享的忽略规则。

本机 `.codex/config.toml` 同步设置这些环境变量，只对受信任项目生效，可能需要新聊天加载；当前聊天和独立终端仍显式使用脚本入口。此配置不移动现有文件，也不改变 Docker Desktop 的磁盘镜像、数据库服务或其他应用的缓存。

Docker 构建和容器测试使用独立存储，`TEMP`、`GOCACHE` 等变量不会改变它。执行这类任务前，应检查 Docker Desktop 磁盘镜像的实际宿主路径，确认位于 E 盘；底层镜像仍在 C 盘时，不在该引擎上生成新的项目构建缓存或测试容器。旧缓存的检查和清理可以正常进行。

若现有 `node_modules` 仍关联旧 pnpm/store，安装可能报 `ERR_PNPM_UNEXPECTED_STORE`。先核对 `frontend/node_modules/.modules.yaml` 中的包管理器和 store 路径；确需迁移且依赖未被使用时，从已载入环境的终端进入 `frontend`，用确定的 pnpm 版本执行 `install --force --frozen-lockfile` 重新关联。复用 E 盘缓存，缺失的包仍需下载；不要手改 `.modules.yaml`，也不要擅自重装正在使用的依赖。

### PostgreSQL 16 (Windows 服务)

以下是历史本机安装位置，服务端口与数据目录应先只读核实；软件安装在 C 盘不代表可以在 C 盘新建项目、测试库或缓存。凭据从本机私有配置读取，不在维护文档保存，也不假设存在默认密码。

| 配置项 | 值 |
|--------|-----|
| 端口 | 5432 |
| psql 路径 | `C:\Program Files\PostgreSQL\16\bin\psql.exe` |
| pg_hba.conf | `C:\Program Files\PostgreSQL\16\data\pg_hba.conf` |
| 数据库凭据 | 从本机私有配置读取；不在文档保存用户名或密码 |
| 超级用户 | 按本机 PostgreSQL 实例配置核实；不得假设默认密码 |

### Redis

| 配置项 | 值 |
|--------|-----|
| 端口 | 6379 |
| 密码 | 按本机 Redis 私有配置核实，不假设无密码或默认值 |

### 开发工具

工具链核对日期：2026-11-11；以下按源码配置记录，不代表本机已经安装或本轮执行过构建。

| 工具 | 当前配置依据 |
| --- | --- |
| Go | `backend/go.mod` 为 **1.27.3**；三个 Dockerfile 的 Go 镜像均为 `1.27.3-alpine` |
| golangci-lint | `backend-ci.yml` 固定 **v2.14.0** |
| Node.js | `backend-ci.yml`、`security-scan.yml`、`release.yml` 为 **24**；根目录及 `deploy/Dockerfile` 默认 **24-alpine** |
| pnpm | 上述 CI 使用 **9**，两个前端 Docker 构建固定 **9.15.9**；`frontend/pnpm-lock.yaml` 的 `lockfileVersion` 为 **9.0**，`package.json` 未设置 `packageManager` 或 `engines` |

安装或切换工具前先加载环境，复用 E 盘缓存。下面的显式 pnpm 调用可避免默认 shim 版本漂移；不要用全局 npm 安装替代项目约定：

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目\backend'
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

Set-Location 'E:\sub2二次开发项目\frontend'
corepack pnpm@9.15.9 --version
```

## 三、CI/CD 流水线

### GitHub Actions Workflows

| Workflow | 触发条件 | 检查内容 |
|----------|----------|----------|
| **backend-ci.yml** | push, pull_request | 部署脚本检查、Go 单元/集成测试（含独立 QQ PostgreSQL）、前端 lint/类型/全量 Vitest、golangci-lint v2.14.0、发布辅助脚本测试 |
| **security-scan.yml** | push, pull_request, 每周一 03:00 UTC | govulncheck + pnpm 生产依赖审计及豁免校验；当前没有独立 gosec 步骤 |
| **release.yml** | tag `v*`、workflow_dispatch | 构建/验证/发布；手动支持 `dry_run` 与 `simple_release`，PR 不触发 |
| **publish-custom-*.yml** | workflow_dispatch | 各版本独立发布配置；从交接页定位对应文件，核对固定源码和精确 SHA 的 CI/安全门禁，再构建及验证 Linux 镜像；不代表服务器已升级 |

本表列主要测试与发布入口；仓库另有 Apple Container、CLA 等工作流，以 `.github/workflows/` 的实际触发条件为准。纯文档维护无需触发构建或发布。

### CI 要求

- Go 当前基线为 **1.27.3**。常规 CI/安全/发布通过 `go-version-file: backend/go.mod` 取版本，并在相应步骤断言 `go1.27.3`。升级时同步检查这些断言、三个 Dockerfile 的 Go 构建镜像，以及待发布 `publish-custom-*.yml` 中单独固定的 `go-version`；历史发布配置只作为当时来源记录，不批量改写。
- 前端使用 `pnpm install --frozen-lockfile`，依赖变更必须同步 `package.json` 与 `pnpm-lock.yaml`。`pnpm build` 依次执行 i18n 完整性检查、`vue-tsc -b`、Vite；`pnpm lint:check` 只读检查，`pnpm lint` 会自动修复文件。
- 根目录 `make test-frontend` 执行 `lint:check`、`typecheck` 和 `FRONTEND_CRITICAL_VITEST` 清单，不是完整前端测试。完整套件使用 `pnpm test:run`；记录验证时区分两者。

### 本地测试命令

按改动范围选择以下命令，不应整段无差别执行；集成测试需先满足隔离数据库与 Docker 的 E 盘存储要求。

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目\backend'
# 后端单元测试
go test -tags=unit ./...

# 后端集成测试
go test -tags=integration ./...

# 代码质量检查
golangci-lint run ./...

# 前端依赖安装（必须用 pnpm）
Set-Location 'E:\sub2二次开发项目\frontend'
corepack pnpm@9.15.9 install --frozen-lockfile
corepack pnpm@9.15.9 lint:check
corepack pnpm@9.15.9 typecheck
corepack pnpm@9.15.9 test:run
```

## 四、常见坑点 & 解决方案

### 坑 1：pnpm-lock.yaml 必须同步提交

**问题**：`package.json` 新增依赖后，CI 的 `pnpm install --frozen-lockfile` 失败。

**原因**：上游 CI 使用 pnpm，lock 文件不同步会报错。

**解决**：
```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目\frontend'
corepack pnpm@9.15.9 install  # 仅在有意更新依赖时更新锁文件
git diff -- package.json pnpm-lock.yaml
```

核对依赖差异后，将锁文件与对应依赖变更一并提交；常规安装仍用 `--frozen-lockfile`。

---

### 坑 2：npm 和 pnpm 的 node_modules 冲突

**问题**：之前用 npm 装过 `node_modules`，pnpm install 报 `EPERM` 错误。

**解决**：先停止占用依赖的开发进程，区分权限占用和 `ERR_PNPM_UNEXPECTED_STORE`。需要重新关联时按上方 E 盘缓存迁移说明处理；确需删除 `node_modules` 时，先核实其解析后的绝对路径在当前 E 盘工作区内，再使用 PowerShell 的 `Remove-Item -LiteralPath`。不要直接复制 `rm -rf` 或清理旧 C 盘联接。

---

### 坑 3：PowerShell 中 bcrypt hash 的 `$` 被转义

**问题**：bcrypt hash 格式如 `$2a$10$xxx...`，PowerShell 把 `$2a` 当变量解析，导致数据丢失。

**解决**：PowerShell 使用单引号 here-string 保留 `$`，将 SQL 写入 E 盘项目临时文件后用 `psql -f` 执行。下面仅演示字面量，不写用户表；真实连接参数按本机私有配置填写。
```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
$sqlFile = Join-Path $env:TEMP 'literal-dollar-example.sql'
@'
SELECT '$2a$10$example' AS literal_text;
'@ | Set-Content -LiteralPath $sqlFile -Encoding utf8
Set-Location -LiteralPath $env:TEMP
psql -h 127.0.0.1 -f '.\literal-dollar-example.sql'
Remove-Item -LiteralPath $sqlFile
```

---

### 坑 4：psql 的路径编码兼容问题

**问题**：部分 Windows 客户端或调用方式使用中文绝对路径时可能报找不到文件，不能据此认定所有 psql 版本都不支持中文路径。

**解决**：把文件放到项目临时目录，进入目录后只向 psql 传相对文件名：
```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Copy-Item -LiteralPath 'E:\sub2二次开发项目\output\file.sql' -Destination (Join-Path $env:TEMP 'psql-input.sql')
Set-Location -LiteralPath $env:TEMP
psql -f '.\psql-input.sql'
Remove-Item -LiteralPath (Join-Path $env:TEMP 'psql-input.sql')
```

---

### 坑 5：PostgreSQL 密码重置流程

先区分服务未启动、连接目标错误、`pg_hba.conf` 规则不匹配和密码失效。端口、服务名、数据目录、角色名都以目标本机实例为准；能连接时只读核对 `SHOW data_directory`、`SHOW hba_file`，无法连接时从对应服务配置定位，不把历史安装表或默认密码当成实际配置。

确需恢复本机实例访问时，先备份实际认证配置。优先通过已有管理连接，用 psql 的 `\password <实际角色名>` 交互设置新密码，避免把明文写入命令历史、SQL 文件或文档。若必须临时调整认证，仅限制到所需本机地址、数据库和角色；完成后立即恢复原认证规则并核对连接，不把所有数据库和角色长期设为 `trust`。同步使用该凭据的本机私有配置，不在维护指南记录其值。正式与 QQ 测试数据库按各自部署交接处理。

---

### 坑 6：Go interface 新增方法后 test stub 必须补全

**问题**：给 interface 新增方法后，编译报错 `does not implement interface (missing method XXX)`。

**原因**：所有测试文件中实现该 interface 的 stub/mock 都必须补上新方法。

**解决**：
```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
# 搜索所有实现该 interface 的 struct
Set-Location 'E:\sub2二次开发项目\backend'
rg -n 'type.*(Stub|Mock).*struct' internal

# 逐一补全新方法
```

---

### 坑 7：Windows 上 psql 连 localhost 的 IPv6 问题

**问题**：psql 连 `localhost` 先尝试 IPv6 (::1)，可能报错后再回退 IPv4。

**建议**：直接用 `127.0.0.1` 代替 `localhost`。

---

### 坑 8：Windows 没有 make 命令

**问题**：CI 里用 `make test-unit`，本地 Windows 没有 make。

**解决**：直接用 Makefile 里的原始命令：
```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目\backend'
# 代替 make test-unit
go test -tags=unit ./...

# 代替 make test-integration
go test -tags=integration ./...
```

---

### 坑 9：Ent Schema 修改后必须重新生成

**问题**：修改 `ent/schema/*.go` 后，代码不生效。

**解决**：
```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目\backend'
go generate ./ent  # 重新生成 ent 代码（json.RawMessage 字段会生成为同类型的 jsontext.Value，属预期）
git diff --stat -- ent  # 审查生成差异，生成文件与 schema 一并提交
```

---

### 坑 10：前端测试看似正常，但后端调用失败（模型映射被批量误改）

**典型现象**：
- 前端按钮点测看起来正常；
- 实际通过 API/客户端调用时返回 `Service temporarily unavailable` 或提示无可用账号；
- 常见于 OpenAI 账号（例如 Codex 模型）在批量修改后突然不可用。

**根因**：
- OpenAI 账号编辑页默认不显式展示映射规则，容易让人误以为“没映射也没关系”；
- 但在**批量修改同时选中不同平台账号**（OpenAI + Antigravity/Gemini）时，模型白名单/映射可能被跨平台策略覆盖；
- 结果是 OpenAI 账号的关键模型映射丢失或被改坏，后端选不到可用账号。

**修复方案（按优先级）**：
1. **快速修复（推荐）**：在批量修改中补回正确的透传映射（例如 `gpt-5.3-codex -> gpt-5.3-codex-spark`）。
2. **彻底重建**：删除并重新添加全部相关账号（最稳但成本高）。

**关键经验**：
- 如果某模型已被软件内置默认映射覆盖，通常不需要额外再加透传；
- 但当上游模型更新快于本仓库默认映射时，**手动批量添加透传映射**是最简单、最低风险的临时兜底方案；
- 批量操作前尽量按平台分组，不要混选不同平台账号。

---

### 坑 11：PR 提交前检查清单

提交 PR 前按改动范围选择本地验证，并注明未执行或条件跳过的原因。纯文档更正检查链接、引用和差异，不运行无关业务测试；涉及数据库或集成测试时先满足隔离环境及 E 盘存储要求：

- [ ] `go test -tags=unit ./...` 通过
- [ ] `go test -tags=integration ./...` 通过
- [ ] `golangci-lint run ./...` 无新增问题
- [ ] 前端变更执行 `lint:check`、`typecheck` 及相关测试；涉及构建时执行 `build`
- [ ] `pnpm-lock.yaml` 已同步（如果改了 package.json）
- [ ] 所有 test stub 补全新接口方法（如果改了 interface）
- [ ] Ent 生成的代码已提交（如果改了 schema）

## 五、常用命令速查

### 数据库操作

以下为独立命令示例，连接身份与数据库名按实际本机配置选择；不要对正式或 QQ 测试实例直接套用本地示例。

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目'
# 连接数据库
psql -U sub2api -h 127.0.0.1 -d sub2api

# 查看所有用户
psql -U postgres -h 127.0.0.1 -c "\du"

# 查看所有数据库
psql -U postgres -h 127.0.0.1 -c "\l"

# 执行 SQL 文件
psql -U sub2api -h 127.0.0.1 -d sub2api -f '.\output\migration.sql'
```

### Git 操作

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目'
# 先核对当前分支、未提交修改和上游差异
git status --short
git branch --show-current
git fetch upstream
git log --oneline HEAD..upstream/main

# 需要隔离开发时，创建 E 盘工作树；进入后仍使用主项目环境入口
git worktree add '.\.worktrees\feature-example' -b feature/example
```

合并、rebase、推送按本次任务范围执行，不把上述只读核对当作自动发布流程。保留原分支与未提交修改；不要重置或清理既有工作树。

### 前端操作

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目\frontend'
# 安装依赖（必须用 pnpm）
corepack pnpm@9.15.9 install --frozen-lockfile

# 开发服务器
corepack pnpm@9.15.9 dev

# 构建
corepack pnpm@9.15.9 build
```

### 后端操作

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目\backend'
# 运行服务器
go run ./cmd/server/

# 生成 Ent 代码
go generate ./ent

# 运行测试
go test -tags=unit ./...
go test -tags=integration ./...

# Lint 检查
golangci-lint run ./...
```

## 六、项目结构速览

```
E:/sub2二次开发项目/
├── backend/
│   ├── cmd/server/          # 主程序入口
│   ├── ent/                 # Ent ORM 生成代码
│   │   └── schema/          # 数据库 Schema 定义
│   ├── internal/
│   │   ├── handler/         # HTTP 处理器
│   │   ├── service/         # 业务逻辑
│   │   ├── repository/      # 数据访问层
│   │   ├── qqimage/         # QQ 付费生图模块
│   │   └── server/          # 服务器配置
│   ├── migrations/          # 数据库迁移脚本
│   └── go.mod               # Go 工具链与依赖
├── frontend/
│   ├── src/
│   │   ├── api/             # API 调用
│   │   ├── components/      # Vue 组件
│   │   ├── views/           # 页面视图
│   │   ├── types/           # TypeScript 类型
│   │   └── i18n/            # 国际化
│   ├── package.json         # 依赖配置
│   └── pnpm-lock.yaml       # pnpm 锁文件（必须提交）
├── deploy/                  # 部署配置与分版本发布记录
├── docs/                    # HANDOFF、AI入口、变更记录
├── tools/                   # 本机环境入口与维护工具
├── CHANGELOG.md             # 项目更新日志
└── DEV_GUIDE.md             # 本文档
```

## 七、参考资源

- [上游仓库](https://github.com/Wei-Shaw/sub2api)
- [Ent 文档](https://entgo.io/docs/getting-started)
- [Vue3 文档](https://vuejs.org/)
- [pnpm 文档](https://pnpm.io/)
