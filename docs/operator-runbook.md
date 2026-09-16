# Open XAI Connect 运维手册

本文面向自托管维护者。首次部署请先阅读 [README](../README.md)；文中的 `connect.example.com` 和 `forum.example.com` 分别代表自己的 Connect 与 Discourse 域名。

## 部署

1. 复制 `.env.example` 为受控目录中的 `.env`，为所有 `replace-with-*` 项生成独立高熵值。不要把 `.env` 提交到 Git。
2. 先运行 `scripts/check-compose.ps1`。
3. 执行数据库迁移和启动：

   ```powershell
   docker compose --env-file .env -f deploy/docker-compose.yml config --quiet
   docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
   docker compose --env-file .env -f deploy/docker-compose.yml exec portal /connect healthcheck
   ```

4. 参考 `deploy/nginx.connect.xai.run.conf` 配置反向代理，替换域名、证书路径和上游。宿主机 Nginx 使用 `http://127.0.0.1:8080`（随 `PORTAL_PORT` 调整）；同一 Docker 网络中的代理可使用 `http://portal:8080`。公开请求均转发至 Portal，并通过 HTTPS 提供服务。Compose 中 Hydra admin `4445` 仅 `expose`，没有公网端口映射。
5. 在 Discourse 安装 `discourse-plugin/connect-identity`，设置 `connect_identity_enabled`、HTTPS `connect_identity_base_url` 和与 Portal 相同的共享密钥，并启用下文的 DiscourseConnect Provider 设置。密钥缺失或 Base URL 非 HTTPS 时插件会 fail closed；首次完成配置后应重启或重建 Discourse。

Compose 不包含 Discourse 或反向代理。`.env.example` 中的原站域名与 Turnstile Site Key 均需替换，所有 `replace-with-*` 项必须配置；Portal 运行用户为 `nonroot`，资源卷或自定义挂载目录应允许该用户写入。

### 已有数据库升级

PostgreSQL 的 `/docker-entrypoint-initdb.d` 只在空数据卷首次初始化时执行。`docker compose up` 或重建 Portal 不会自动升级已有的 Portal 表结构；Hydra 的迁移由单独的 `hydra-migrate` 服务负责。

升级前备份数据库及密钥，按文件编号检查并执行尚未应用的 Portal 迁移，再启动新版本。例如，已应用 `001` 和 `002` 的实例升级到支持按应用配置 PKCE + nonce 的版本时：

```powershell
docker compose --env-file .env -f deploy/docker-compose.yml exec postgres psql -v ON_ERROR_STOP=1 -U connect -d connect -f /docker-entrypoint-initdb.d/003_application_pkce_nonce.sql
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build portal
```

将示例中的数据库用户和名称替换为自己的 `POSTGRES_USER`、`POSTGRES_DB`，并确保 PostgreSQL 容器已挂载对应迁移文件。不要通过删除数据卷触发初始化来升级已有实例。

### 部署验证

`/readyz` 只检查 PostgreSQL 和 Redis；Compose 的 Hydra 健康检查执行 `hydra version`，不能证明 OIDC 请求已可用。启动后还需检查 Discovery，并用测试应用完成登录、授权码换 Token 和 UserInfo 请求。

Portal 的 DiscourseConnect 待完成登录状态位于进程内。重启后正在进行的登录可能需要重新发起；部署多个 Portal 副本前，需要额外设计登录状态共享。

## 日常检查

- `GET /healthz` 只表示进程存活；`GET /readyz` 同时检查 PostgreSQL 和 Redis。
- 观察 Hydra migration 是否成功、Portal 日志中的 provisioning 重试次数、outbox 积压和身份事件验签失败。
- 升级到支持邮箱的版本时，先执行 `migrations/002_user_email.sql`（`users.email` 为非空字符串，已有用户默认空值），再重启 Portal；同时更新并重启 Discourse 的 `connect-identity` 插件。
- 确认反向代理不缓存 `/oauth2`、`/userinfo`、`/.well-known`、登录、consent、Secret 和撤销响应。
- 审核权限来自 Discourse `connect-reviewers` 群组；`connect-admins` 成员自动拥有全部审核权限（包含自己创建的应用）和运营概览权限。两组标记只用于 Portal 内部授权，不进入 OIDC Claim。

## 备份与恢复

- 定期备份 PostgreSQL，并单独备份 `portal-assets` 卷。
- PostgreSQL 备份与 `CONNECT_ENCRYPTION_KEY`、Hydra 系统密钥、Cookie 密钥、Discourse shared secret 分开保管；缺少任一密钥时不要尝试恢复生产数据库。
- 恢复顺序：PostgreSQL → Redis（可丢失短期 Session/nonce）→ Hydra migration → Portal → 反向代理。恢复后检查 `/readyz`、Discovery 和一个测试 Client 的 PKCE 流程。

## 密钥轮换

- Hydra system/cookie/pairwise secret 和 Discourse shared secret 不能直接覆盖。先在维护窗口备份并准备回滚值，再按 Hydra/Discourse 的双密钥或短暂停机策略切换，完成健康检查后撤销旧值。
- `CONNECT_ENCRYPTION_KEY` 用于数据库中的 Client Secret 加密。当前 MVP 使用单一活动密钥；轮换前必须导出受控备份并安排逐条重加密任务，不能只修改环境变量，否则旧密文将无法解密。轮换任务完成后再删除旧密钥备份。

## 故障处理

- Hydra admin 不可用：应用审核会停在 `provisioning`，outbox worker 会重试；不要手工在 Hydra 创建同名 Client。
- Discourse 不可用：不能建立新的 Connect 身份；已有状态同步失败时，授权和 UserInfo fail closed。
- Redis 不可用：新登录、CSRF、Session 和 nonce 相关操作失败；修复 Redis 后无需清理 PostgreSQL outbox。
- 发现 Secret 出现在日志或工单中：立即在 Portal 轮换 Client Secret，撤销相关 Hydra Session，保留不含凭证正文的审计记录。

## Discourse 管理员操作

这里的 `forum.example.com` 指 Discourse 论坛；第三方应用的审核入口仍在 `connect.example.com`。如果已经按本项目完成部署，Portal、插件和 Nginx 已配置好，管理员主要确认群组和成员即可。

### 首次配置

在 Discourse 管理后台完成以下检查：

1. 确认 `connect-identity` 插件已安装并启用。
2. 在站点设置中搜索 `connect_identity`，确认：

   | 设置 | 建议值 |
   | --- | --- |
   | `connect_identity_enabled` | `true` |
   | `connect_identity_base_url` | `https://connect.example.com` |
   | `connect_identity_shared_secret` | 与 Portal 的 `DISCOURSE_SHARED_SECRET` 完全相同 |
   | `connect_identity_reviewer_groups` | `connect-reviewers`（可用 `\|` 添加多个群组） |
   | `connect_identity_admin_groups` | `connect-admins`（可用 `\|` 添加多个群组） |
   | `connect_identity_request_window_seconds` | 默认 `300`；跨地域或网络较慢时可适当增大 |

   共享密钥只应通过服务器密钥管理或受控运维渠道配置，不要粘贴到工单、聊天、浏览器或代码仓库。

3. 确认 Discourse 内置 Connect Provider 设置：

   - `enable_discourse_connect_provider = true`；
   - `discourse_connect_provider_secrets` 包含 `connect.example.com|同一共享密钥`；
   - `discourse_connect_allowed_redirect_domains` 包含 `connect.example.com`。

4. 在“管理后台 → 群组”创建并维护：

   - `connect-reviewers`：允许成员审核 Connect 应用；
   - `connect-admins`：Connect 管理员标记，供管理级功能使用。

   新创建的应用默认免人工审核，Portal 会自动排队创建 OIDC Client。`connect-reviewers` 和 `connect-admins` 仍可打开审核队列处理历史或特殊的 `pending_review` 应用；`connect-admins` 成员还可以访问“运营概览”，并在满足账号状态要求时绕过 TL1 创建应用。两组身份标记都会同步到 Portal，不要求管理员通过 Hydra Admin API 手工创建客户端。

首次完成插件配置后需要重启或重建 Discourse，以加载插件路由与事件钩子；插件代码或 `app.yml` 变化时也需重建。群组成员调整通常即时生效。

### 历史待审核应用

1. 审核员或管理员使用自己的 Discourse 账号打开 <https://connect.example.com/connect/review>。
2. 查看待审核应用的名称、用途、回调地址和已验证域名。
3. 必须填写审核理由，然后选择“批准并上线”“要求修改”或“驳回”。
4. 批准后 Portal 会异步创建 OIDC Client；默认自动上线的应用不需要经过这一步。

普通审核员不能审核自己创建的应用；`connect-admins` 成员可以审核所有应用，包括自己创建的应用。管理员不需要登录 Hydra、手工创建 OAuth Client，也不应直接修改 Portal 数据库。

### 账号和权限维护

- 用户的账号、密码、二步验证、活跃/暂停/禁言状态仍在 Discourse 管理；插件会将必要状态同步给 Portal。
- 要取消审核权限，需从所有授予审核权限的群组中移除用户，包括 `connect_identity_reviewer_groups` 和 `connect_identity_admin_groups` 配置的群组；管理员同样具有审核权限。
- 要阻止某用户继续登录，按 Discourse 的正常流程停用或暂停账号。
- 如需轮换 Portal 与 Discourse 之间的共享密钥，必须同时更新两端并安排短暂维护窗口。
