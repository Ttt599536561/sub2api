# OAuth 模型测试提示需要新版 Codex

## 现象与原因

在后台使用 OpenAI / ChatGPT OAuth 订阅账号测试 `gpt-6-astra` 时，上游可能返回：

```text
API returned 400: {"detail":"The 'gpt-6-astra' model requires a newer version of Codex. Please upgrade to the latest app or CLI and try again."}
```

该请求由 Sub2API 后端发往 ChatGPT Codex 接口，客户端身份来自后端的
`User-Agent` 和 `version` 请求头。更新操作人员电脑上的 Codex 不会改变这些请求头。

修复前的内置版本是 `0.146.0`。尚未同步、读取设置失败时会使用此版本；已保存的旧同步值
也会优先于内置版本。后者意味着只更新内置常量，仍可能继续发送旧身份。

[官方更新日志](https://learn.chatgpt.com/docs/changelog)记载 `0.153.1` 加入 Astra 支持，
`0.155.1` 于 2026-09-18 发布。本次以内置 `0.155.1` 作为更新后的默认兼容基线，
并在自动选择版本时使用同步值和内置值中较新的一个。UA 与 `version` 仍由同一生效版本生成。

## 部署与设置

重新构建并部署包含修复的后端后，再测试原来的 OAuth 账号和 `gpt-6-astra`。

如果系统设置中的“Codex 客户端版本号”手动填写了旧值，需要清空该字段并开启
“自动同步 Codex 版本号”，或将手动值更新为支持 Astra 的版本。显式填写的版本仍具有最高优先级，
本修复不会改写管理员保存的设置。

对于尚未部署修复的实例，可临时把“Codex 客户端版本号”设为 `0.155.1` 后重试。
升级并恢复自动同步后建议清空手动版本，避免长期固定版本再次落后。

## 回归验证

- 网站 OAuth 模型测试在无解析器、无同步值、旧同步值、设置读取失败时使用支持 Astra 的身份。
- `User-Agent` 与 `version` 相同，模型名称仍为 `gpt-6-astra`。
- 较新的同步版本继续生效；旧同步值不再降低内置版本；显式固定版本仍优先。

测试使用本地上游桩检查实际构造的出站请求，不访问真实 OAuth 凭据。
生产实例是否已生效，应以部署后的账号测试结果为准。
