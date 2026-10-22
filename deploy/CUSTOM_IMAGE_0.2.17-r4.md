# Sub2API 二开0.2.17-r4发布与升级记录

日期：2026-10-23（Asia/Shanghai）。用户授权CI通过后发布amd64/arm64镜像，不部署生产；本文件当前为发布准备记录。

## 固定来源与门禁

- 镜像目标：`ghcr.io/ttt599536561/sub2api-custom:0.2.17-r4`，尚未声明发布成功。
- 卡片功能来源：`6f90164d5b55584f8266764d2d26f5c6a9bd1a9d`。
- 发布源码 / OCI revision固定为安全修复提交：`76ff7c88a0e792b39936346db9c9707930eba0ef`。
- 专用工作流：[publish-custom-v0217-r4.yml](../.github/workflows/publish-custom-v0217-r4.yml)。工作流控制提交只负责发布，checkout使用上述固定源码。
- 新源码最新main/push CI与Security Scan必须均成功；采用原生Ubuntu amd64/arm64 runner构建及运行检查，不使用macOS，不覆盖已存在r4标签。
- 每架构核验版本、源码revision、实际二进制Go1.27.3 build info、PG客户端与定价资源、隔离启动/setup/status及前端资源后才合成最终索引。隔离安装检查的needs_setup=true不代表生产应如此。

原提交因Go1.27.2和follow-redirects1.16.0安全告警未满足门禁；用户明确批准最小修复后改用新SHA。实际修复与本地验证见[安全补丁记录](../docs/changes/2026-10-23-r4-security-patches.md)，卡片功能见[实施记录](../docs/changes/2026-10-23-monitor-cards.md)。不得把旧提交的失败安全检查算作新提交成功。

## 升级边界

生产当前为r3，本轮不自动部署。r3到r4不新增SQL迁移、必需环境变量或推广入口配置，已有迁移247和现有持久数据继续保留。升级时只修改现有Compose的sub2api镜像，不替换整份Compose或删除数据卷。最终镜像地址、digest、验证结果与可执行升级步骤在发布完成后补入本页。
