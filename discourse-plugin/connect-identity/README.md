# Discourse Connect Identity Bridge

这是 connect.xai.run 的薄 Discourse 插件。Discourse 仍负责账号、密码、二步验证和账号状态；插件只提供经过 HMAC-SHA256 验签的最小用户状态，并把状态变更事件发布给 Portal。

## 配置

1. 在 Discourse 管理后台安装插件。
2. 配置 connect_identity_enabled、connect_identity_base_url 和 connect_identity_shared_secret。
3. 设置 reviewer/admin 群组名称，例如 connect-reviewers|staff。
4. 将同一密钥配置到 Portal 的 DISCOURSE_SHARED_SECRET，通过 HTTPS 部署。

当插件开关打开但 Base URL 不是 HTTPS 或共享密钥为空时，插件会记录告警并保持接口、事件钩子均关闭（fail closed），不会以未签名或半配置状态提供身份数据。

服务接口：

- GET /connect/identity/users/:id

状态变更由插件后台任务主动 POST 到 Portal 的 `/connect/identity/events`；Discourse 端不开放一个会把事件再转发回 Portal 的同名入口，避免事件回环。

两个接口都要求 X-Connect-Timestamp、X-Connect-Nonce 和 X-Connect-Signature。响应包含 Discourse ID、用户名、昵称、邮箱、头像、信任等级和账号状态，以及仅供 Portal 后台访问控制使用的 connect_reviewer / connect_admin 布尔值；不会返回任意群组、外部账号或 API Key。邮箱只会在 Connect 已批准 `email` Scope 后通过 OIDC 返回给第三方应用。

在 Discourse 容器中运行插件测试：

~~~bash
bundle exec rspec plugins/connect-identity/spec
~~~
