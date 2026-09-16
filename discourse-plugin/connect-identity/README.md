# PureConnect Discourse Identity Bridge

这是 [PureConnect](../../README.md) 的 Discourse 身份桥接插件，可连接自行部署的 PureConnect Portal。Discourse 仍负责账号、密码、二步验证和账号状态；插件只提供经过 HMAC-SHA256 验签的最小用户状态，并把状态变更事件发布给 Portal。

## 安装

源码位于 [puremixai/pureconnect](https://github.com/puremixai/pureconnect) 的 `discourse-plugin/connect-identity` 子目录。将该子目录作为 Discourse 的 `plugins/connect-identity` 安装，确保 `plugins/connect-identity/plugin.rb` 存在。仓库根目录不是可直接安装的 Discourse 插件目录。

容器部署时，应将获取仓库并复制此子目录的步骤纳入自己的 Discourse 构建配置，使插件在重建后仍然存在。安装或更新插件代码后重建 Discourse。

## 配置

1. 在 Discourse 站点设置中将 `connect_identity_enabled` 设为 `true`。
2. 将 `connect_identity_base_url` 设为自己的 Portal HTTPS 地址，例如 `https://connect.example.com`，并配置 `connect_identity_shared_secret`。
3. 设置 reviewer/admin 群组名称，默认分别为 `connect-reviewers`、`connect-admins`；多个群组用 `|` 分隔。
4. 将同一密钥配置到 Portal 的 `DISCOURSE_SHARED_SECRET`，并配置 DiscourseConnect Provider；完整设置见[运维手册](../../docs/operator-runbook.md)。
5. 首次完成配置后重启或重建 Discourse，确保插件路由和事件钩子加载。

当插件开关打开但 Base URL 不是 HTTPS 或共享密钥为空时，插件会记录告警并保持接口、事件钩子均关闭（fail closed），不会以未签名或半配置状态提供身份数据。

服务接口：

- GET /connect/identity/users/:id
- GET /connect/identity/users/:id/level-progress

状态变更由插件后台任务主动 POST 到 Portal 的 `/connect/identity/events`；Discourse 端不开放一个会把事件再转发回 Portal 的同名入口，避免事件回环。

两个接口都要求 X-Connect-Timestamp、X-Connect-Nonce 和 X-Connect-Signature。

- `GET /connect/identity/users/:id` 保持现有最小身份状态响应，包含 Discourse ID、用户名、昵称、邮箱、头像、信任等级和账号状态，以及仅供 Portal 后台访问控制使用的 `connect_reviewer` / `connect_admin` 布尔值。
- `GET /connect/identity/users/:id/level-progress` 仅供 Portal 服务端调用，返回当前等级、下一等级、升级模式、目标等级条件、阻断条件和生成时间。TL0→TL3 的条件在 Discourse 内部按现有 `Promotion` / `TrustLevel3Requirements` 规则计算；TL3→TL4 返回手动授予；TL4 返回无下一等级。

等级进度接口不会返回任意群组、外部账号、帖子/话题原始记录、IP、API Key 或其他敏感论坛数据。邮箱只会在 PureConnect 已批准 `email` Scope 后通过 OIDC 返回给第三方应用；等级进度接口本身不返回邮箱或其他身份资料。

在 Discourse 容器中运行插件测试：

~~~bash
bundle exec rspec plugins/connect-identity/spec
~~~
