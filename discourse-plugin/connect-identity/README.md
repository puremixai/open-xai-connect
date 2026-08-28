# Discourse Connect Identity Bridge

这是 connect.xai.run 的薄 Discourse 插件。Discourse 仍负责账号、密码、二步验证和账号状态；插件只提供经过 HMAC-SHA256 验签的最小用户状态，并把状态变更事件发布给 Portal。

## 配置

1. 在 Discourse 管理后台安装插件。
2. 配置 connect_identity_enabled、connect_identity_base_url 和 connect_identity_shared_secret。
3. 设置 reviewer/admin 群组名称，例如 connect-reviewers|staff。
4. 将同一密钥配置到 Portal 的 DISCOURSE_SHARED_SECRET，通过 HTTPS 部署。

服务接口：

- GET /connect/identity/users/:id
- POST /connect/identity/events

两个接口都要求 X-Connect-Timestamp、X-Connect-Nonce 和 X-Connect-Signature。响应只包含 Discourse ID、用户名、昵称、头像、信任等级和账号状态，以及仅供 Portal 后台访问控制使用的 connect_reviewer / connect_admin 布尔值；不会返回邮箱、任意群组、外部账号或 API Key。

在 Discourse 容器中运行插件测试：

~~~bash
bundle exec rspec plugins/connect-identity/spec
~~~
