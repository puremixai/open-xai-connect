# XAI Connect 接入指南

XAI Connect 是一个标准 OIDC Provider。用户账号、密码、二步验证和账号状态仍由 Discourse 管理，接入方只保存自己的本地会话，不应把 Connect Access Token 当作本地登录 Cookie。

## 1. 创建应用

1. 使用 Discourse 账号登录 `https://connect.xai.run`。
2. 在“我的应用”中创建草稿，填写用途、Logo、精确 HTTPS 回调地址和已验证域名。
3. 保存应用。普通用户需要 TL1 及以上、活跃且未被禁言；`connect-admins` 群组成员在账号活跃、未被禁言且未暂停时可绕过 TL1 创建。每个用户最多三个开放中的应用。
4. 应用默认免人工审核，平台自动进入 Provisioning；成功后页面显示 Client ID。Client Secret 默认掩码；所有者完成近期敏感操作确认后可以查看或轮换。

回调地址不允许通配符、localhost、IP、非 HTTPS 或未被已验证域名覆盖的主机。Secret 只能存放在服务端密钥管理系统中，不能写入浏览器、源码或日志。

## 2. OIDC Authorization Code + PKCE

Discovery 地址：

```text
https://connect.xai.run/.well-known/openid-configuration
```

接入方应先读取 Discovery，不要硬编码端点。每次登录：

1. 生成至少 256 bit 的随机 `code_verifier`，计算 `S256` 的 `code_challenge`。
2. 生成不可预测的 `state` 和 OIDC `nonce`，在自己的短期会话中保存。
3. 将浏览器重定向到 `authorization_endpoint`，参数至少包含 `client_id`、精确 `redirect_uri`、`response_type=code`、`scope=openid profile community`、`state`、`nonce`、`code_challenge` 和 `code_challenge_method=S256`。
4. 回调时校验 `state`，在服务端用 `client_secret_basic` 调用 `token_endpoint`，同时提交原始 `code_verifier`。
5. 校验 ID Token 的签名、`iss`、`aud`、`exp`、`nonce`，再建立接入方自己的本地会话。
6. 需要资料时使用短期 Access Token 调用 `userinfo_endpoint`，并在本地会话结束时调用 `revocation_endpoint`。

允许的 Scope 为 `openid`、`profile`、`community` 和可选的 `offline_access`。只有本次 Token 实际获批的 Scope 对应 Claim 才会返回：

| Scope | Claims |
| --- | --- |
| `openid` | `sub` |
| `profile` | `preferred_username`、`name`、`picture` |
| `community` | `trust_level`、`active`、`silenced` |

不会返回邮箱、群组、Discourse ID、管理员标记、外部账号或 API Key。账号被停用/封禁后，新的登录和 UserInfo 会失败；禁言用户仍可作为普通用户登录，但会看到 `silenced=true`。

## 3. 刷新和退出

请求 `offline_access` 后，服务端可以使用 `refresh_token` 获取新 Token。Refresh Token 使用 idle/absolute 双期限并轮换；如果返回新的 Refresh Token，立即替换旧值，旧值不得重复使用。

Access Token、Refresh Token、授权码和 Connect Session 都有平台侧生命周期；接入方仍必须设置自己的本地 Session 超时和撤销策略。

## 4. 最小服务端示例

本仓库的 [examples/go-client/main.go](/D:/bbs/xai-connect/examples/go-client/main.go) 展示 Discovery、PKCE URL、授权码换 Token、UserInfo 和撤销请求。示例仅读取环境变量，不包含真实凭证：

```powershell
$env:CONNECT_ISSUER_URL = "https://connect.xai.run"
go run ./examples/go-client discovery
go run ./examples/go-client authorize
go run ./examples/go-client exchange <code> <verifier>
go run ./examples/go-client userinfo <access-token>
go run ./examples/go-client revoke <refresh-token>
```
