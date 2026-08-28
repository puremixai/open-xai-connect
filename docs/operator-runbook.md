# XAI Connect 运维手册

## 部署

1. 复制 `.env.example` 为受控目录中的 `.env`，为所有 `replace-with-*` 项生成独立高熵值。不要把 `.env` 提交到 Git。
2. 先运行 `scripts/check-compose.ps1`。
3. 执行数据库迁移和启动：

   ```powershell
   docker compose --env-file .env -f deploy/docker-compose.yml up -d
   docker compose --env-file .env -f deploy/docker-compose.yml exec portal /connect healthcheck
   ```

4. 在反向代理加载 `deploy/nginx.connect.xai.run.conf`，只把 HTTPS 443 暴露到公网。Compose 中 Hydra admin `4445` 仅 `expose`，没有公网端口映射。
5. 在 Discourse 安装 `discourse-plugin/connect-identity`，设置 `connect_identity_enabled`、HTTPS `connect_identity_base_url` 和与 Portal 相同的共享密钥。密钥缺失或 Base URL 非 HTTPS 时插件会 fail closed。

## 日常检查

- `GET /healthz` 只表示进程存活；`GET /readyz` 同时检查 PostgreSQL 和 Redis。
- 观察 Hydra migration 是否成功、Portal 日志中的 provisioning 重试次数、outbox 积压和身份事件验签失败。
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
