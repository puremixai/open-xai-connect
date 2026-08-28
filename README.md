# XAI Connect

XAI Connect 是基于 Discourse 账号的独立身份认证平台，对外提供标准的 OpenID Connect（OIDC）服务。

接入方不需要接触 Discourse 密码、Cookie 或 Discourse API Key，只需要把用户重定向到 XAI Connect，完成授权码 + PKCE 登录，再用 Access Token 获取用户资料。

当前线上地址：<https://connect.xai.run>

## 接入前提

XAI Connect 当前面向服务端 Web 应用，使用 OIDC Authorization Code + PKCE：

- 接入方必须拥有服务端，用于保存 Client Secret、`state`、`nonce`、`code_verifier` 和本地 Session。
- Client Secret 不能放入浏览器、移动端安装包、前端源码、日志或 URL。
- 每个回调地址必须是精确的 HTTPS URL，不支持通配符、`localhost`、IP 地址、查询参数或片段。
- 只有已审核通过的应用才能完成生产登录。

如果你的应用是 SPA 或移动端，建议增加自己的后端/BFF，由后端完成 Token Exchange；当前平台的客户端注册契约是机密客户端（`client_secret_basic`）。

## 1. 创建应用并获取凭证

1. 使用 Discourse 账号打开 <https://connect.xai.run>，进入“我的应用”。
2. 创建应用草稿，填写：
   - 应用名称和用途说明；
   - 一个或多个精确的 HTTPS 回调地址；
   - 能覆盖所有回调主机的已验证域名；
   - 可选的 PNG/JPEG Logo。
3. 保存草稿并提交审核。
4. 审核通过后，平台异步创建 OIDC Client，应用状态变为 `approved`，页面显示 Client ID。
5. 应用所有者在完成近期敏感操作确认后查看 Client Secret。Secret 可以明文显示，请立即保存到服务端密钥管理系统。

应用创建者必须是活跃、未被禁言且 Trust Level（TL）不低于 1 的 Discourse 用户。每个用户最多有 3 个处于开放状态的应用。

### 回调地址示例

```text
https://app.example.com/auth/xai/callback
```

以下地址会被拒绝：

```text
http://app.example.com/auth/callback       # 非 HTTPS
https://*.example.com/auth/callback        # 通配符
https://localhost:3000/callback            # localhost
https://192.0.2.10/callback                # IP 地址
https://app.example.com/callback?next=/    # 查询参数
```

## 2. OIDC 端点

接入方应优先读取 Discovery，不要在代码中散落硬编码端点：

```text
GET https://connect.xai.run/.well-known/openid-configuration
```

当前端点如下：

| 用途 | 地址 |
| --- | --- |
| Discovery | `https://connect.xai.run/.well-known/openid-configuration` |
| Authorization | `https://connect.xai.run/oauth2/auth` |
| Token | `https://connect.xai.run/oauth2/token` |
| UserInfo | `https://connect.xai.run/userinfo` |
| Revocation | `https://connect.xai.run/oauth2/revoke` |
| JWKS | `https://connect.xai.run/.well-known/jwks.json` |

支持的协议能力：

- Response Type：`code`
- Grant Type：`authorization_code`、`refresh_token`
- Token Endpoint Auth：`client_secret_basic`
- PKCE：仅 `S256`
- Access Token：不透明 Token，不要尝试把它当作 JWT 解码

## 3. 登录流程：Authorization Code + PKCE

### 3.1 生成一次性参数

每次登录都生成新的值，并保存在接入方自己的短期 Session 中：

- `code_verifier`：至少 256 bit 随机值；
- `code_challenge`：`BASE64URL(SHA256(code_verifier))`；
- `state`：防 CSRF；
- `nonce`：绑定 ID Token，防重放。

伪代码：

```text
code_verifier  = random_bytes(32).base64url()
code_challenge = base64url(sha256(code_verifier))
state          = random_bytes(24).base64url()
nonce          = random_bytes(24).base64url()

session.oauth = {
  code_verifier,
  state,
  nonce,
  redirect_uri: "https://app.example.com/auth/xai/callback"
}
```

### 3.2 重定向用户

将浏览器重定向到下面的地址。`redirect_uri` 必须与创建应用时登记的值完全一致：

```text
https://connect.xai.run/oauth2/auth
  ?client_id=YOUR_CLIENT_ID
  &redirect_uri=https%3A%2F%2Fapp.example.com%2Fauth%2Fxai%2Fcallback
  &response_type=code
  &scope=openid%20profile%20community
  &state=YOUR_STATE
  &nonce=YOUR_NONCE
  &code_challenge=YOUR_CODE_CHALLENGE
  &code_challenge_method=S256
```

实际代码应使用 URL 编码库构造参数，不要手工拼接未编码的用户输入。

用户会在 XAI Connect 页面完成 Discourse 登录和授权。授权成功后，平台将浏览器带回：

```text
https://app.example.com/auth/xai/callback?code=AUTHORIZATION_CODE&state=YOUR_STATE
```

### 3.3 回调校验和换 Token

回调接口必须先校验 `state`，确认通过后再用服务端凭证调用 Token Endpoint。使用 HTTP Basic 发送 `client_id:client_secret`，不要把 Secret 放在 URL 中：

```bash
curl -sS -X POST "https://connect.xai.run/oauth2/token" \
  -u "${CONNECT_CLIENT_ID}:${CONNECT_CLIENT_SECRET}" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "grant_type=authorization_code" \
  --data-urlencode "code=${AUTHORIZATION_CODE}" \
  --data-urlencode "redirect_uri=https://app.example.com/auth/xai/callback" \
  --data-urlencode "code_verifier=${CODE_VERIFIER}"
```

典型响应包含：

```json
{
  "access_token": "opaque-access-token",
  "token_type": "bearer",
  "expires_in": 86400,
  "scope": "openid profile community",
  "id_token": "eyJ..."
}
```

如果请求了 `offline_access` 并获得用户同意，响应还会包含 `refresh_token`。

### 3.4 校验 ID Token 并建立本地 Session

接入方必须使用 OIDC 库完成以下校验：

1. 使用 Discovery 中的 `jwks_uri` 验证签名；
2. 校验 `iss` 等于 `https://connect.xai.run`；
3. 校验 `aud` 包含自己的 Client ID；
4. 校验 `exp`、`iat` 和 `nonce`；
5. 使用 ID Token 中的 `sub` 作为本地用户的外部唯一标识。

验证成功后，建立接入方自己的本地 Session。不要把 XAI Connect Access Token 直接当作本地登录 Cookie。

## 4. 获取用户资料

使用 Access Token 调用 UserInfo：

```bash
curl -sS "https://connect.xai.run/userinfo" \
  -H "Authorization: Bearer ${ACCESS_TOKEN}"
```

返回的 Claim 由用户实际批准的 Scope 决定：

| Scope | Claim |
| --- | --- |
| `openid` | `sub` |
| `profile` | `preferred_username`、`name`、`picture` |
| `community` | `trust_level`、`active`、`silenced` |

示例：

```json
{
  "sub": "usr_xxx",
  "preferred_username": "alice",
  "name": "Alice",
  "picture": "https://...",
  "trust_level": 2,
  "active": true,
  "silenced": false
}
```

平台不会通过 OIDC 返回邮箱、Discourse ID、群组、管理员标记、外部账号或 API Key。用户状态发生变化后，新的登录和 UserInfo 请求会按当前 Discourse 状态处理。

建议按以下规则映射用户：

- 用 `sub` 作为唯一键，不要用用户名作为唯一键；
- `preferred_username` 和 `name` 只作为展示字段；
- 每次需要授权操作时，根据 Scope 判断 Claim 是否存在；
- 对 `active=false` 或 UserInfo 返回 401 的用户停止创建新业务 Session。

## 5. 刷新和退出

### 刷新 Token

仅在确实需要长期离线访问时请求 `offline_access`。Refresh Token 必须只保存在服务端，并按响应中的新值轮换：

```bash
curl -sS -X POST "https://connect.xai.run/oauth2/token" \
  -u "${CONNECT_CLIENT_ID}:${CONNECT_CLIENT_SECRET}" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "grant_type=refresh_token" \
  --data-urlencode "refresh_token=${REFRESH_TOKEN}"
```

如果返回新的 `refresh_token`，立即替换旧值；旧值不得再次使用。Access Token、Refresh Token 和 Session 的具体有效期由平台配置控制，接入方仍应配置自己的本地 Session 超时。

### 撤销 Token

用户退出或解绑时，先删除接入方自己的 Session，再按需撤销 Token：

```bash
curl -sS -X POST "https://connect.xai.run/oauth2/revoke" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "token=${ACCESS_OR_REFRESH_TOKEN}"
```

## 6. Scope 申请建议

按最小权限原则申请 Scope：

```text
openid profile
```

只有需要社区状态时再申请：

```text
openid profile community
```

只有需要离线刷新时再追加：

```text
openid profile community offline_access
```

用户拒绝某个 Scope、平台未授予某个 Scope，或历史 Consent 未包含某个 Scope 时，接入方必须能够处理 Claim 缺失。

## 7. 错误处理

接入方至少应处理以下情况：

- 用户取消授权：回调带有 `error=access_denied`；
- `state` 不匹配：立即终止流程，不换 Token；
- Token Endpoint 返回 `invalid_grant`：授权码过期、已使用或 PKCE 不匹配；
- Token Endpoint 返回 `invalid_client`：Client ID/Secret 错误；
- UserInfo 返回 `401`：Access Token 无效、过期，或用户当前不可认证；
- 回调地址不一致：检查登记的精确 HTTPS URL、域名覆盖和反向代理配置。

错误页面和日志中不要输出 Client Secret、Authorization Code、Access Token、Refresh Token 或完整的回调查询字符串。

## 8. 最小 Go 示例

仓库中已有一个不含真实凭证的命令行示例：[`examples/go-client/main.go`](examples/go-client/main.go)。

```powershell
$env:CONNECT_ISSUER_URL = "https://connect.xai.run"
$env:CONNECT_CLIENT_ID = "your-client-id"
$env:CONNECT_CLIENT_SECRET = "your-client-secret"
$env:CONNECT_REDIRECT_URI = "https://app.example.com/auth/xai/callback"

go run ./examples/go-client discovery
go run ./examples/go-client authorize
go run ./examples/go-client exchange <code> <code-verifier>
go run ./examples/go-client userinfo <access-token>
go run ./examples/go-client revoke <token>
```

`authorize` 命令会输出授权 URL 和需要保留的 `code_verifier`；生产环境应把这些值放在短期服务端 Session 中，而不是让用户手工复制。

## 9. 平台部署者说明

如果需要自行部署 Portal、Hydra、PostgreSQL、Redis 和 Discourse 插件，请参阅：

- [`docs/integration-guide.md`](docs/integration-guide.md)
- [`docs/operator-runbook.md`](docs/operator-runbook.md)
- [`deploy/docker-compose.yml`](deploy/docker-compose.yml)

Discourse 仍然是用户账号、密码、二步验证和账号状态的来源；XAI Connect 只维护必要的身份映射、应用、授权和审计数据，不提供第二套密码系统。

## License

内部项目，许可证按仓库所有者的约定执行。
