# OpenAI 分组强制 Ultrafast 设计方案

- **日期**：2026-10-17（Asia/Shanghai）
- **状态**：待用户复核；本阶段只提交设计，不修改业务代码
- **需求基线**：`b9c36c6ee`
- **实现工作树**：`E:\sub2二次开发项目\.worktrees\sub2api-upstream-20261015`（当前为干净的二开合并工作树，包含 `b9c36c6ee`）

## 1. 概述

在 Sub2 管理员后台的分组编辑/创建表单中，为 **OpenAI 平台分组**增加“强制使用 Ultrafast”开关。

开关开启后：

1. 只对 `gpt-6.1-sol` 与 `gpt-6-astra` 两个目标模型生效；
2. 目标模型的出站请求强制使用 `service_tier=ultrafast`；
3. 仅该分组的目标模型 Ultrafast 用量按 **Standard 有效基价 × 8** 计费；
4. 其他模型、其他分组和未开启分组的既有计费行为保持不变；
5. “强制 Ultrafast”与现有“强制 Fast（priority）”互斥；“免费 Fast”保持独立，且不减免 Ultrafast。

这里的“只针对某一个分组”是计费和请求策略的共同边界：不能通过修改全局模型价格表把所有分组的 Ultrafast 倍率改成 8 倍。

## 2. 已确认的产品口径

### 2.1 分组范围

- 管理端控件只在 `openai` 平台分组显示和保存。
- `composite` 分组不启用本开关，即使现有 Fast 控件支持 Composite，也不把 Ultrafast 扩展到 Composite。
- 非 OpenAI 分组在后端归一化时强制保存为 `false`，防止旧客户端或手工 API 请求写入无效配置。

### 2.2 互斥关系

- `force_openai_ultrafast=true` 时，`force_openai_fast` 必须为 `false`。
- `force_openai_fast=true` 时，`force_openai_ultrafast` 必须为 `false`。
- 前端切换一个开关时，立即关闭另一个开关。
- 后端保存时再次归一化，不能只依赖前端保证。
- `free_openai_fast` 不参与上述互斥；它仍只对 `priority/fast` 请求按 Standard 客户价计算，不能让 Ultrafast 免费。
- 若收到不可信旧客户端提交的两个强制开关同时为 `true`，后端以 Ultrafast 为优先并清除 Fast，保证最终不存在双重强制模式。

### 2.3 模型范围

- 目标规范模型名为：
  - `gpt-6.1-sol`
  - `gpt-6-astra`
- 使用现有 OpenAI 模型规范化/别名解析逻辑后再判断，保留项目已有的路径前缀、日期后缀和公开别名兼容能力；不新增宽泛的 GPT-6 家族匹配。
- `gpt-6` 现有别名若由项目当前规范化逻辑归一为 Astra，则按 Astra 处理；`gpt-6-sol`、`gpt-6-luna` 等其他模型不处理。
- 非目标模型不主动添加、删除或改写 `service_tier`，也不套用本功能的 8 倍计费。

### 2.4 请求行为

- 开启 Ultrafast 的 OpenAI 分组调用目标模型时，无论入站 `service_tier` 为空、`priority/fast`、`flex` 或其他已识别值，均按“强制 Ultrafast”改写为 `ultrafast`。
- 目标模型在未开启分组中不被本开关改写；用户自行传入 `ultrafast` 时，保持当前版本原有请求和计费语义，不套用新增的分组 8 倍规则。
- 目标模型在已开启分组中，即使 Ultrafast 来自用户显式请求而不是开关自动写入，只要最终仍按 Ultrafast 结算，也属于该分组的 Ultrafast 计费范围。
- 现有全局 OpenAI Fast/Flex 策略继续运行。分组强制值先写入，再经过现有策略的过滤、拦截或改写；因此全局策略仍可阻止或改变最终 tier。最终计费以既有的响应 tier 降级结算逻辑为准。
- **不做账号类型或上游能力预探测**：不因为 API Key、OAuth、Base URL 或能力清单不同而跳过强制，也不自动降级、改回普通 tier 或静默重试。只要分组和模型命中，就发送 `service_tier=ultrafast`；如果上游不支持，由上游错误原样进入现有错误处理链路。
- HTTP、Chat Completions/Responses 转换、原生透传、回退路径和 WebSocket（包括现有 v1/v2 入口）必须使用同一套判断逻辑，避免不同协议出现不同 Ultrafast 行为。
- 管理员保存后，新请求立即读取新配置；已建立的 WebSocket 会话沿用项目当前策略快照/重连语义，不承诺中途改变已建立会话。

### 2.5 计费行为

计费条件必须同时满足：

1. 请求所属分组为 OpenAI 分组；
2. 该分组已开启强制 Ultrafast；
3. 最终计费模型归一后为 `gpt-6.1-sol` 或 `gpt-6-astra`；
4. 最终用于计费的 `service_tier` 仍为 `ultrafast`。

满足时：

- 使用该请求当前有效的 Standard 基价作为基准，乘以 8；
- 分组倍率、用户×分组倍率、峰值倍率以及现有有效定价来源继续按原顺序处理；
- 输入、输出、缓存读取、缓存写入等 token 费用统一采用同一 Ultrafast 8 倍口径；
- 使用日志继续记录真实的 `service_tier=ultrafast`，不引入伪造的 tier 名称；
- `free_openai_fast` 不作用于 Ultrafast。

不满足时：

- 已开启分组但目标模型最终为 Standard/Fast/Flex：按相应现有 tier 计费，不额外乘 8；
- 未开启分组，即使用户手动传 Ultrafast：沿用 `b9c36c6ee` 当前既有模型/通用 Ultrafast 计费规则，不改为 8 倍；
- 其他模型：完全沿用既有规则。

“Standard 基价”指本次请求实际选定的有效定价来源在 Standard tier 下的价格，包括现有分组/渠道/内置解析优先级；不会读取另一个分组的价格，也不会修改全局模型目录中的 Ultrafast 倍率。

## 3. 方案比较与选择

### 方案 A：请求级、分组感知的计费覆盖（推荐）

保留现有全局 `ModelPricing.UltrafastMultiplier` 和未开启分组的旧行为；在 OpenAI token 计费请求中显式携带“本次是否命中开启 Ultrafast 分组”的派生标记，计费层仅对满足条件的目标模型和 Ultrafast tier 使用 8 倍覆盖。

**优点**：

- 不污染全局模型价格；
- 未开启分组的手动 Ultrafast 行为完全兼容；
- 计费条件可测试、可审计，和请求所属分组明确绑定；
- 可复用现有有效 Standard 定价解析，支持分组/渠道定价。

**代价**：

- 需要把分组作用域从 OpenAI 用量记录路径传递到通用 token 计费入口；
- 需要同时覆盖 HTTP、WS 和请求失败/降级后的最终 tier 结算测试。

### 方案 B：直接把全局 Ultrafast 倍率改为 8

把 Astra 或 Ultrafast 的全局模型倍率改成 8。

**不采用原因**：会影响所有分组，无法满足“只有指定分组额外收费”；也会改变未开启分组用户手动传 Ultrafast 的既有费用。

### 方案 C：新增伪造的内部 tier，例如 `ultrafast_group_8x`

用内部 tier 名称区分分组 Ultrafast，再在上游发送前映射回 `ultrafast`。

**不采用原因**：会污染用量日志、响应 tier、降级比较、前端统计和既有 tier 解析；容易在一条协议路径漏掉映射，维护成本高于请求级计费覆盖。

## 4. 交互设计

沿用现有管理员分组的“OpenAI Fast 模式”区块，推荐顺序：

1. 强制使用 Fast（priority）
2. 强制使用 Ultrafast（新增）
3. 免费 Fast

新增开关文案建议：

- 标题：`强制使用 Ultrafast`
- 说明：`开启后，此 OpenAI 分组中仅 GPT-6.1 Sol 与 GPT-6 Astra 请求会强制使用 Ultrafast，并按 Standard 基价的 8 倍计费；其他模型不变。与强制 Fast 互斥；免费 Fast 不适用于 Ultrafast。保存后新请求生效，已建立的 WebSocket 会话需按现有规则重连。`

创建和编辑表单都需要：

- 显示开关当前值；
- 切换时同步清除互斥开关；
- 在切换平台离开 OpenAI 时清除并禁用该值；
- 按现有分组设置样式、国际化和 `data-testid` 测试模式实现。

不新增单独的模型选择器或倍率输入框：模型范围固定为两个目标模型，倍率固定为 8，避免管理员配置出无法验证的组合。

## 5. 技术设计

### 5.1 持久化与数据流

新增字段：

```text
groups.force_openai_ultrafast BOOLEAN NOT NULL DEFAULT FALSE
```

实现范围：

- Ent Group schema 字段、生成代码和运行时字段；
- 下一可用编号的追加迁移 SQL，使用 `IF NOT EXISTS` 并带字段注释；
- Group domain/service model；
- 管理员创建/更新 DTO、handler、service、repository；
- 管理端 group mapper、API 类型和创建/编辑表单；
- API Key 认证缓存/快照和按 Key 查询的分组投影；
- 分组复制、fixture、迁移结构测试及管理员接口测试。

已有记录的历史迁移不修改，只追加新迁移。

### 5.2 强制请求策略

新增一个集中判断函数，输入可信分组上下文、最终出站模型和请求体，输出是否需要将 `service_tier` 设置为 `ultrafast`。函数至少保证：

- 非 OpenAI 平台、无可信分组上下文、开关关闭、非目标模型均不改写；
- 目标模型命中时覆盖现有 tier；
- 不改变模型字段、计费模型选择和用户原始请求审计字段；
- 复用现有 `normalizedOpenAIServiceTierValue`、模型别名规范化和策略错误处理。
- 不调用账号能力清单来决定是否强制；API Key、OAuth 和其他 OpenAI 账号类型统一走同一改写逻辑。

将该判断接入现有 Fast policy 的公共入口，而不是在每个 handler 中复制一份 `if`，并覆盖：

- Responses/Chat Completions 转换路径；
- 原生 Chat Completions 透传路径；
- OpenAI Responses 透传/回退路径；
- WebSocket 首帧及后续 `response.create` 帧；
- 已有路由提示和使用日志中的最终 tier。

执行顺序：

```text
解析/映射最终模型
→ 分组强制 Ultrafast（仅目标模型）
→ 现有全局 Fast/Flex policy
→ 上游转发
→ 记录请求 tier 与上游观察 tier
→ 既有降级解析
→ 分组感知的 8× token 计费
```

### 5.3 分组感知计费

推荐在现有 `TokenCostRequest` / `CostInput` 链路增加一个窄用途的请求级覆盖字段，或等价的内部计费上下文：

```text
OpenAIUltrafastGroupPricing bool
```

该标记只能由服务端根据 `apiKey.Group`、最终计费模型和最终 service tier 派生，不能接受前端或用户请求体直接传入。

计费层处理原则：

1. 先按现有 Resolver 得到本次有效 Standard 定价；
2. 只在标记为真、模型为两个目标模型、tier 为 `ultrafast` 时，对克隆的有效定价应用 `8.0` Ultrafast multiplier；
3. 不改写共享缓存、全局 fallback 表或其他分组使用的定价对象；
4. 继续通过现有 `CostBreakdown` 写入 TotalCost、ActualCost、usage log 和余额/订阅扣减；
5. `free_openai_fast` 的 Standard 重算路径明确排除 `ultrafast`，避免出现“8×后又免费”的组合。

如果请求的上游响应声明了更低 tier，先沿用当前 `ResolveOpenAIServiceTierBilling` 逻辑降低最终计费 tier；只有最终 tier 仍为 Ultrafast 才套用本分组 8×覆盖。

## 6. 测试与验收

### 后端单元/集成测试

- 分组创建、更新、读取和复制正确保存 `force_openai_ultrafast`；
- 非 OpenAI 平台归零；OpenAI 分组两个强制开关互斥；脏请求同时为真时 Ultrafast 优先；
- API Key 认证缓存和请求上下文能携带新字段；
- 目标模型规范名、现有别名/后缀命中；非目标模型不命中；
- 开关开启时目标模型的空 tier、priority、flex 均改为 Ultrafast；关闭时请求体字节/字段保持原语义；
- HTTP、原生透传、回退和 WS 首帧/后续帧行为一致；
- 全局 policy 的 filter/block/force 行为继续生效；
- API Key、OAuth 等不同账号类型均强制发送 Ultrafast；不支持的上游返回错误，系统不做能力探测或自动降级；
- 开启分组 + 目标模型 + 最终 Ultrafast = Standard × 8；
- 开启分组但最终 Standard/Fast/Flex 不乘 8；
- 未开启分组手动 Ultrafast 沿用旧倍率；
- 其他模型、其他分组、`free_openai_fast` 行为不回归；
- 分组/渠道自定义 Standard 价格、分组倍率、峰值倍率、缓存 token 和上游降级均按预期组合；
- 使用日志仍记录 `service_tier=ultrafast`，不出现内部伪 tier。

### 前端测试

- 创建/编辑表单显示新开关且正确回填；
- Fast 与 Ultrafast 切换互相关闭；Free Fast 不被误关；
- 非 OpenAI/Composite 平台不发送新字段或发送后归一为关闭；
- 中文、英文文案和提示说明覆盖目标模型、8 倍计费、互斥及 Free Fast 边界；
- 现有分组保存测试、类型检查和构建通过。

### 验收场景

至少用一个启用分组和一个未启用分组分别验证：

1. 两个目标模型在启用组被强制 Ultrafast，用户费用为有效 Standard 价的 8 倍；
2. 同样的两个模型在未启用组不被强制，手动 Ultrafast 不使用新增 8 倍；
3. 普通模型在启用组也不被改写；
4. Fast/Ultrafast 不能同时开启；
5. 余额扣减、订阅额度、用量日志和管理员用量展示口径一致。

## 7. 非目标范围

- 不把 Ultrafast 8 倍改成全局模型价格；
- 不修改未开启分组的旧 Ultrafast 计费；
- 不给 Composite 或其他平台增加该开关；
- 不让免费 Fast 覆盖 Ultrafast；
- 不新增管理员自定义 Ultrafast 倍率或模型列表；
- 不修改历史迁移；
- 不把本功能扩展到非 OpenAI 的 `service_tier`、速度档或其他供应商协议。

## 8. 成功标准

- 管理员能在 OpenAI 分组中独立开启 Ultrafast，并看到明确的 8×、模型范围和互斥提示；
- 同一模型在不同分组按各自开关得到正确出站 tier 和费用；
- 未开启分组既有行为保持兼容；
- 请求、WS、计费、日志和前端设置不存在分叉口径；
- 新增迁移可在已有数据库上安全执行；
- 相关 Go 单测/集成测试、前端测试、类型检查和构建在实现工作树通过。

## 9. 已确认的实现选择

1. **“强制”覆盖入站 tier**：已开启分组的两个目标模型即使客户端传入 `priority` 或 `flex`，也先改为 `ultrafast`；之后仍接受现有全局 Fast/Flex policy 的过滤、拦截或改写。
2. **不预探测账号能力**：不区分 API Key、OAuth 或其他 OpenAI 账号类型；命中分组和模型后都发送 `ultrafast`。上游不支持时返回上游错误，不自动降级或静默改回普通 tier。
3. **现有模型别名兼容**：沿用项目已有规范化逻辑，`gpt-6` 等已明确归为 Astra 的别名按 Astra 处理；不额外扩展未知别名。
