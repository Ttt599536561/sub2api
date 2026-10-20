# 专属推广子域名启用说明

此说明适用于已包含专属推广域名功能的新源码。代码、镜像发布、DNS/证书和生产启用是不同步骤；仅增加解析记录不会给旧版应用增加邀请归因。

## 1. 配置应用主站与推广账号

1. 在管理员设置中将前端地址设为主站 HTTPS origin，例如 `https://api.example.test`。允许末尾 `/`，不填写路径、查询、片段或显式端口（包括 `:443`）。
2. 开启邀请返利并核对现有返利比例、期限、上限、冻结期。
3. 在邀请返利的“配置专属域名”中按完整邮箱查找既有推广用户，再填写 `partner.api.example.test`。不要只凭邮箱前缀或猜测数字用户 ID 保存。
4. 保存后该用户邀请页显示 `https://partner.api.example.test/`。域名与邀请码/返利比例独立保存；清空域名会撤销入口及尚未完成的相关归因，不影响已绑定用户。

专属域名必须是主站 hostname 的严格子域名。一个推广用户同时拥有一个启用域名；地址没有业务上的固定到期日，持续可用仍依赖父域名、解析、证书和应用配置。30 天是一次访问的归因期限，不是专属链接的寿命。

## 2. DNS 与证书

在父域 `example.test` 的 DNSPod 解析中添加记录：

| 字段 | 示例值 |
| --- | --- |
| 主机记录 | `partner.api` |
| 类型 | `CNAME` |
| 记录值 | `api.example.test` |
| 线路 | 默认 |
| TTL | 600 秒 |

上例适合主站直接进入同一台服务器/同一入口的部署。若主站前接 CDN、WAF 或负载均衡，也要在该服务接入新 hostname；或改用 A 记录指向实际应用入口的公网 IP。CNAME 只是解析，不是网页跳转。字段含义见 [腾讯云 DNSPod 主机记录与记录值](https://intl.cloud.tencent.com/zh/document/product/1295/77014)。

为 `partner.api.example.test` 配置 HTTPS 证书，并安排续期。通配符 `*.example.test` 不覆盖 `partner.api.example.test`；使用准确 hostname 的证书，或覆盖相应层级的证书。腾讯云提供 [Nginx 证书部署说明](https://intl.cloud.tencent.com/zh/document/product/1007/30954)。

## 3. 反向代理

主站和推广域名都必须把原始访问 hostname 传给同一 Sub2API 后端，尤其主站的所有 `/api/v1/auth/` 路径。不要在 DNS、Nginx 或面板中直接将推广域名 301/302 到主站注册页，否则应用收不到来源。

以下为 Nginx 示例。证书路径和 upstream 端口应使用该服务器实际配置；`127.0.0.1:8080` 只是常见本机 Docker 端口示例。

```nginx
server {
    listen 80;
    server_name partner.api.example.test;
    return 301 https://partner.api.example.test$request_uri;
}

server {
    listen 443 ssl;
    server_name partner.api.example.test;
    ssl_certificate     /etc/nginx/certs/partner.api.example.test/fullchain.pem;
    ssl_certificate_key /etc/nginx/certs/partner.api.example.test/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_no_cache 1;
        proxy_cache_bypass 1;
    }
}
```

`Host` 必须保留推广 hostname；应用不信任 `X-Forwarded-Host` 来决定推广人。Nginx 的该项行为见 [proxy_set_header 官方文档](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_set_header)。同时在后端 `server.trusted_proxies` 配置实际反代的 IP/CIDR，避免所有访客被计为同一个代理 IP 并共享入口限流额度；不要把全网设为可信。

主站的 `/api/v1/auth/affiliate/claim` 必须转到后端；一次性票据在 query 中，反代/CDN/错误追踪不得记录该 query，也不得缓存交接响应。主站可针对该路径关闭访问日志或使用只包含 `$uri`、不包含 `$args`/`$request` 的日志格式。例如，在现有主站 HTTPS server 块内按当前 upstream 参数加入：

```nginx
location = /api/v1/auth/affiliate/claim {
    access_log off;
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $http_host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
    proxy_no_cache 1;
    proxy_cache_bypass 1;
}
```

修改后先运行服务器现有的 Nginx 配置检查，再按其部署方式重载。使用宝塔、1Panel、CLB 或 Caddy 时应用相同的 hostname、TLS、无缓存与日志规则，不要直接粘贴 Nginx 语法到其他服务。

## 4. 应用升级与迁移

- 前后端一起部署；后端启动应成功应用新增迁移 `247_affiliate_promotion_hosts.sql`。该迁移新增映射表及删除用户时的失效处理，不修改历史 SQL。
- Redis 必须可用：交接票据有效两分钟；主站不透明归因记录最多保留 30 天。Redis 故障不会退回无归因注册；从原推广域名重新进入可恢复已过期/撤销的访问流程。
- 用户创建、邀请绑定与第三方注册完成共用数据库事务；既有账号登录和账号绑定不会新增或改写邀请人。
- 推广域名停用、改绑或用户删除时版本递增，旧票据/浏览器归因/pending 注册不会转移到新推广人。hostname 墓碑和 revision 不得手工删除或重置；恢复已软删除用户也不会自动重新启用域名，须管理员重新设置。
- 回退到不支持此功能的旧版本会丢失尚未注册的浏览器归因。应先停用推广入口，并明确处理仍在进行的注册；必要时临时关闭新注册。已完成的邀请关系保存在原有表中，不需要改写或删除。

## 5. 上线验收

1. 使用干净浏览器访问完整推广子域名，确认 HTTPS 无错误，最终进入主站干净的 `/register`，地址没有 `aff` 参数。
2. 开发者工具中确认主站归因状态为 `locked: true`，注册表单不显示返佣邀请码；后端不返回推广人的邀请码或 ID。
3. 完成邮箱注册/邮箱验证注册；在管理员邀请记录中核对新用户的邀请人。再对实际启用的每类第三方登录执行新账号注册。
4. 用同一浏览器先后进入不同有效推广域名，确认已有有效来源保留；修改 URL/请求中的 `aff_code` 不能覆盖它。
5. 现有账号登录不改绑；无推广来源的普通主站注册仍按原流程工作。
6. 用新的测试账号按现有充值规则核对返利流水，不用管理员手工改数据库伪造验收结果。
7. 撤销测试域名后旧链接和未完成归因应明确报失效，不能默默注册成无邀请账号。

不要将本机单元/集成测试当作线上 DNS、证书和实际支付的验收。
