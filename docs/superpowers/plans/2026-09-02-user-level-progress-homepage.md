# 首页用户等级进度展示 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 PureConnect 现有首页增加 Discourse 驱动的用户等级进度面板，展示当前等级、下一等级以及达到下一等级的条件和当前进度。

**Architecture:** Discourse 插件新增受现有 HMAC 保护的 level-progress 接口，在 Discourse 内复用当前等级升级规则计算目标等级条件。Portal 通过独立的 identity progress contract 获取并校验数据，用 Redis 缓存 5 分钟，在 /connect/apps 首页服务端渲染新增面板；现有身份状态接口、页面骨架和数据库模型保持不变。

**Tech Stack:** Go 1.25、标准库 net/http、html/template、PostgreSQL 用户映射、Redis go-redis/v9、Discourse Ruby/Rails 插件、RSpec、现有 Portal CSS。

**Spec:** docs/superpowers/specs/2026-09-02-user-level-progress-homepage-design.md

## Global Constraints

- 本需求只增加首页展示内容，不调整 Portal 的整体结构。
- 接口语义是通用用户等级进度，不是 TL3 专用统计接口，也不是原始统计接口。
- requirements 始终表示达到 next_level 的条件，不表示已经达到当前等级的条件。
- 条件由 Discourse 根据当前站点配置和官方等级规则计算，Portal 不复制或猜测等级规则。
- 浏览器不直接访问 Discourse 进度接口；仅由 Portal 使用 HMAC 服务请求调用。
- 第一版不新增 PostgreSQL 表；等级进度只使用 Redis 短期缓存。
- Redis 成功响应缓存 5 分钟，缓存 key 为 connect:identity:level-progress:v1:<discourse_id>。
- Discourse 现有 GET /connect/identity/users/:id 响应字段集合必须保持不变。
- 当前等级 3 的目标等级为 4，返回 promotion_mode: "manual"；当前等级 4 返回 next_level: null。
- Go 和 Ruby 文件均使用 UTF-8；不得把密钥、原始帖子内容、IP、API Key 或未授权用户资料写入响应、日志或缓存。
- 保留现有未跟踪的 .workbuddy/ 内容，不修改或加入提交。

---

## 文件与职责映射

| 文件 | 职责 |
|---|---|
| internal/identity/level_progress.go | 进度响应模型、接口、响应校验和按 subject 查询用户映射的 lookup |
| internal/identity/level_progress_cache.go | Redis 进度缓存及缓存失效 |
| internal/identity/level_progress_test.go | Portal progress lookup、校验和缓存行为测试 |
| internal/identity/level_progress_cache_test.go | Redis 缓存序列化、命中、过期和失效测试 |
| internal/identity/client.go | 新增签名 GET 客户端方法，保持现有 FetchUser 行为 |
| internal/identity/client_test.go | 新接口路径、签名和非法响应测试 |
| internal/identity/events.go | 状态事件成功更新用户后清理等级进度缓存 |
| internal/identity/events_test.go | 缓存失效调用测试 |
| cmd/connect/main.go | 构造 Redis cache、progress lookup 和首页依赖，传入事件消费者 |
| discourse-plugin/connect-identity/lib/connect_identity/level_progress_serializer.rb | 在 Discourse 内计算目标等级及条件 |
| discourse-plugin/connect-identity/app/controllers/connect_identity/users_controller.rb | 新增 level_progress action |
| discourse-plugin/connect-identity/plugin.rb | 加载序列化器并注册新路由 |
| discourse-plugin/connect-identity/config/locales/server.zh_CN.yml | 等级进度条件标签和模式文案 |
| discourse-plugin/connect-identity/spec/requests/connect_identity_spec.rb | 新接口契约与规则分支 RSpec |
| discourse-plugin/connect-identity/README.md | 新接口的服务端调用和响应范围说明 |
| internal/apps/http.go | 仅在首页列表分支取得可选 LevelProgress |
| internal/apps/http_test.go | 首页成功/失败降级和详情页不调用进度 lookup 测试 |
| internal/web/templates.go | 条件分组、单位、比较方式和进度显示模板函数 |
| internal/web/templates/app-list.html | 在现有统计卡片之后插入等级进度面板 |
| internal/web/static/portal.css | 面板、条件列表、进度条和响应式样式 |
| internal/web/templates_test.go | 面板内容、降级、转义和现有结构保留测试 |

---

### Task 1: 建立 Portal 等级进度契约并实现 Discourse 客户端

**Files:**
- Create: internal/identity/level_progress.go
- Modify: internal/identity/client.go
- Test: internal/identity/level_progress_test.go
- Test: internal/identity/client_test.go

**Interfaces:**
- Produces identity.LevelProgressSnapshot for the cache, handler, and template.
- Produces identity.LevelProgressProvider:

~~~go
type LevelProgressProvider interface {
    FetchLevelProgress(context.Context, int64) (LevelProgressSnapshot, error)
}
~~~

- Produces identity.LevelProgressLookup:

~~~go
type LevelProgressLookup interface {
    CurrentLevelProgress(context.Context, string) (LevelProgressSnapshot, error)
}
~~~

- Do not add FetchLevelProgress to the existing Provider interface, so current status fakes and StatusLookup consumers remain source-compatible.

- [ ] **Step 1: Write failing model and client tests**

Add a fixture with the approved response and assert that the new client method sends a signed GET to the exact path:

~~~go
func TestClientFetchesLevelProgressWithSignedGET(t *testing.T) {
    var gotPath, gotSignature string
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotPath = r.URL.RequestURI()
        gotSignature = r.Header.Get("X-Connect-Signature")
        w.Header().Set("Content-Type", "application/json")
        _, _ = io.WriteString(w, `{"schema_version":1,"discourse_id":42,"current_level":{"id":1,"key":"basic","label":"基础用户"},"next_level":{"id":2,"key":"member","label":"成员"},"promotion_mode":"automatic","requirements_met":false,"requirements":[],"blocking_conditions":[],"generated_at":"2026-09-02T08:00:00Z"}`)
    }))
    defer server.Close()

    client, err := NewClient(server.URL, []byte("shared-secret"), server.Client())
    if err != nil {
        t.Fatalf("NewClient() error = %v", err)
    }
    _, err = client.FetchLevelProgress(context.Background(), 42)
    if err != nil {
        t.Fatalf("FetchLevelProgress() error = %v", err)
    }
    if gotPath != "/connect/identity/users/42/level-progress" || gotSignature == "" {
        t.Fatalf("request path/signature = %q/%q", gotPath, gotSignature)
    }
}
~~~

Add validation tests for schema_version other than 1, mismatched discourse_id, a current level outside 0..4, inconsistent next_level, unknown promotion_mode, automatic without requirements_met, more than 64 requirements, and negative metric values.

- [ ] **Step 2: Run the focused tests to verify they fail**

Run:

~~~powershell
go test ./internal/identity -run 'Test(ClientFetchesLevelProgress|LevelProgressSnapshot)' -count=1
~~~

Expected: FAIL because the progress types, validation, and client method do not exist.

- [ ] **Step 3: Implement the typed response contract**

Create these exact fields in internal/identity/level_progress.go:

~~~go
type LevelProgressSnapshot struct {
    SchemaVersion      int                      `json:"schema_version"`
    DiscourseID        int64                    `json:"discourse_id"`
    CurrentLevel       LevelInfo                `json:"current_level"`
    NextLevel          *LevelInfo               `json:"next_level"`
    PromotionMode      string                   `json:"promotion_mode"`
    RequirementsMet    *bool                    `json:"requirements_met"`
    Requirements       []LevelRequirement       `json:"requirements"`
    BlockingConditions []LevelBlockingCondition `json:"blocking_conditions"`
    GeneratedAt        time.Time                `json:"generated_at"`
}

type LevelInfo struct {
    ID    int    `json:"id"`
    Key   string `json:"key"`
    Label string `json:"label"`
}

type LevelRequirement struct {
    Key        string `json:"key"`
    Label      string `json:"label"`
    Group      string `json:"group"`
    Scope      string `json:"scope"`
    PeriodDays *int   `json:"period_days"`
    Current    int64  `json:"current"`
    Target     int64  `json:"target"`
    Operator   string `json:"operator"`
    Unit       string `json:"unit"`
    Met        bool   `json:"met"`
}

type LevelBlockingCondition struct {
    Key   string `json:"key"`
    Label string `json:"label"`
    Met   bool   `json:"met"`
}
~~~

Use actual Go struct tag syntax with backticks in the source file. Implement ValidateFor(discourseID int64) error. It must require schema 1, current level 0..4, next level current+1 below level 4, no next level at level 4, valid modes automatic/manual/locked/none, a non-nil requirements_met only for automatic, allowed scopes account_lifetime/rolling_period/all_time, operators at_least/at_most/equals, units count/days/minutes, nonnegative values, nonempty keys/labels, and no more than 64 requirements.

- [ ] **Step 4: Implement the signed client method**

Add Client.FetchLevelProgress(ctx, discourseID) in internal/identity/client.go. Reuse the existing nonce generation, HMAC signing, 10-second HTTP client, response status handling, and 1 MiB response limit. Build the path /connect/identity/users/<id>/level-progress, decode JSON into LevelProgressSnapshot, and call ValidateFor before returning.

Keep FetchUser response decoding and validation unchanged. If a private signed-JSON helper is introduced, preserve the current headers and error categories.

- [ ] **Step 5: Run the focused tests to verify they pass**

~~~powershell
go test ./internal/identity -run 'Test(ClientFetchesLevelProgress|LevelProgressSnapshot)' -count=1
go test ./internal/identity -count=1
~~~

Expected: PASS, including existing identity client and signature tests.

- [ ] **Step 6: Commit the contract and client**

~~~powershell
git add -- internal/identity/level_progress.go internal/identity/level_progress_test.go internal/identity/client.go internal/identity/client_test.go
git commit -m "feat: add identity level progress client"
~~~

### Task 2: Add Redis caching, subject lookup, event invalidation, and runtime wiring

**Files:**
- Create: internal/identity/level_progress_cache.go
- Create: internal/identity/level_progress_cache_test.go
- Modify: internal/identity/level_progress.go
- Modify: internal/identity/events.go
- Modify: internal/identity/events_test.go
- Modify: cmd/connect/main.go

**Interfaces:**
- Consumes LevelProgressProvider and store.UserRepository from Task 1.
- Produces LevelProgressCache:

~~~go
type LevelProgressCache interface {
    Get(context.Context, int64) (LevelProgressSnapshot, bool, error)
    Set(context.Context, LevelProgressSnapshot, time.Duration) error
    Invalidate(context.Context, int64) error
}
~~~

- Produces NewLevelProgressRefresher(provider LevelProgressProvider, users store.UserRepository, cache LevelProgressCache) *LevelProgressRefresher.
- LevelProgressRefresher.CurrentLevelProgress(ctx, subject) resolves the local user mapping, reads the cache, fetches and validates on a miss, and caches successful responses.

- [ ] **Step 1: Write failing cache and lookup tests**

Use memory.NewUserRepository() and a fake provider with a call counter. Assert the first lookup fetches Discourse, the second lookup is served from cache, a different subject uses its own Discourse ID, and a response with the wrong discourse_id is rejected before caching.

Add a cache spy for event invalidation:

~~~go
type progressCacheSpy struct {
    invalidated []int64
}

func (s *progressCacheSpy) Invalidate(_ context.Context, discourseID int64) error {
    s.invalidated = append(s.invalidated, discourseID)
    return nil
}
~~~

Extend the existing status event test so a successful event for user 42 records invalidation of ID 42. Keep the existing status persistence and replay assertions.

- [ ] **Step 2: Run focused tests to verify they fail**

~~~powershell
go test ./internal/identity -run 'Test(LevelProgress|EventConsumer)' -count=1
~~~

Expected: FAIL because the cache, refresher, and event invalidation dependency do not exist.

- [ ] **Step 3: Implement the Redis cache**

Create RedisLevelProgressCache with constructor:

~~~go
func NewRedisLevelProgressCache(client *redis.Client, prefix string, ttl time.Duration) *RedisLevelProgressCache
~~~

Use connect:identity:level-progress:v1:<discourse_id> when prefix is empty. Get treats redis.Nil as a miss, decodes JSON, and returns other Redis/JSON errors. Set uses the supplied TTL and defaults to five minutes when TTL is non-positive. Invalidate calls Redis DEL and returns Redis errors.

Keep the Redis client behind a small private backend interface so cache tests can use an in-memory fake without a live Redis server or a new test dependency:

~~~go
type levelProgressCacheBackend interface {
    Get(context.Context, string) (string, error)
    Set(context.Context, string, string, time.Duration) error
    Delete(context.Context, string) error
}
~~~

- [ ] **Step 4: Implement the subject-based refresher**

Add LevelProgressRefresher to internal/identity/level_progress.go. It must:

1. Reject an uninitialized provider or user repository.
2. Resolve subject through users.GetBySubject(ctx, domain.UserID(subject)).
3. Read the cache by user.DiscourseID when configured.
4. Call FetchLevelProgress and ValidateFor(user.DiscourseID) on a miss.
5. Write only validated successful responses to the cache.
6. Return Redis read/write errors and Discourse errors to the caller; the homepage handles the error as display degradation.

- [ ] **Step 5: Add cache invalidation to status events**

Change NewEventConsumer to accept a fourth LevelProgressCache argument and store it as optional. After the existing user upsert succeeds, call Invalidate(ctx, event.UserID) when non-nil. Treat invalidation as best effort so a Redis outage does not discard an already-applied status event; the five-minute TTL remains the fallback.

Update all constructor call sites and existing tests to pass nil when no cache is needed.

- [ ] **Step 6: Wire the runtime**

In cmd/connect/main.go, after redisClient and the Discourse client are created, add:

~~~go
levelProgressCache := identity.NewRedisLevelProgressCache(
    redisClient,
    "connect:identity:level-progress:v1:",
    5*time.Minute,
)
levelProgress := identity.NewLevelProgressRefresher(discourse, db, levelProgressCache)
~~~

Pass levelProgressCache to NewEventConsumer and pass levelProgress to application HTTP dependencies in Task 4. Keep the existing status refresher and event endpoint behavior unchanged.

- [ ] **Step 7: Run the identity suite**

~~~powershell
go test ./internal/identity ./cmd/connect -count=1
~~~

Expected: PASS without requiring a live Redis server for unit tests.

- [ ] **Step 8: Commit caching and runtime support**

~~~powershell
git add -- internal/identity/level_progress.go internal/identity/level_progress_cache.go internal/identity/level_progress_cache_test.go internal/identity/events.go internal/identity/events_test.go cmd/connect/main.go
git commit -m "feat: cache user level progress"
~~~

### Task 3: Implement the generic Discourse level-progress endpoint

**Files:**
- Create: discourse-plugin/connect-identity/lib/connect_identity/level_progress_serializer.rb
- Modify: discourse-plugin/connect-identity/app/controllers/connect_identity/users_controller.rb
- Modify: discourse-plugin/connect-identity/plugin.rb
- Modify: discourse-plugin/connect-identity/config/locales/server.zh_CN.yml
- Modify: discourse-plugin/connect-identity/spec/requests/connect_identity_spec.rb
- Modify: discourse-plugin/connect-identity/README.md

**Interfaces:**
- Consumes the existing ConnectIdentity::SignedRequest before action and User/UserStat/Discourse trust-level methods.
- Produces GET /connect/identity/users/:id/level-progress with the approved JSON fields.
- Does not change UserSerializer#status_fields or the existing user status route.

- [ ] **Step 1: Write failing request specs for all level branches**

Keep the existing approved-user-fields example untouched. Add a request example for a TL0 user with low counters and low TL1 settings:

~~~ruby
it "returns target level one requirements for a new user" do
  user = Fabricate(:user, trust_level: 0)
  user.user_stat.update!(topics_entered: 1, posts_read_count: 2, time_read: 60)
  SiteSetting.tl1_requires_topics_entered = 5
  SiteSetting.tl1_requires_read_posts = 30
  SiteSetting.tl1_requires_time_spent_mins = 10

  path = "/connect/identity/users/#{user.id}/level-progress"
  get path, headers: signed_headers("GET", path)

  expect(response).to have_http_status(:ok)
  expect(response.parsed_body.fetch("current_level").fetch("id")).to eq(0)
  expect(response.parsed_body.fetch("next_level").fetch("id")).to eq(1)
  expect(response.parsed_body.fetch("requirements").map { |item| item.fetch("key") }).to include(
    "topics_entered", "posts_read", "time_read_minutes", "account_age_minutes"
  )
end
~~~

Add target 2 and target 3 cases that assert target-specific keys, current/target values, operator, and met. Add cases for current level 3 to target 4 with manual mode and nil requirements_met; current level 4 with no next level; manual_locked_trust_level with locked mode; bad signature, replay; and absence of groups, posts, IP, API key, and arbitrary user fields.

- [ ] **Step 2: Run plugin specs to verify the new cases fail**

Run from the Discourse environment:

~~~powershell
bundle exec rspec plugins/connect-identity/spec/requests/connect_identity_spec.rb
~~~

Expected: old status examples pass and new level-progress examples fail because the route and serializer are absent.

- [ ] **Step 3: Implement the serializer envelope and level dispatch**

Create ConnectIdentity::LevelProgressSerializer with to_h. Use @user.trust_level as authoritative current level and calculate next_level as current + 1 below 4. Build level info from TrustLevel.levels and TrustLevel.name.

Use these exact mode semantics:

~~~ruby
return "none" if current_level >= 4
return "locked" if @user.manual_locked_trust_level.present?
return "manual" if next_level == 4
"automatic"
~~~

For automatic targets, requirements_met is the conjunction of all numeric requirements and blocking conditions. For manual, locked, and none it is nil.

- [ ] **Step 4: Implement target-level 1 (TL1) and 2 (TL2) conditions**

For target 1, use the same values and settings as Promotion.tl1_met?: topics_entered, posts_read_count, time_read / 60, and account age in minutes. For target 2, add days_visited, likes_received, likes_given, and calc_topic_reply_count!, using the corresponding tl2_requires_* settings.

Emit each metric with key, localized label, group, scope, period_days, current, target, operator, unit, and met. Use minutes for time_read_minutes and account_age_minutes, days for days_visited, and count for other counters.

- [ ] **Step 5: Implement target-level 3 (TL3) conditions using Discourse’s rule object**

Instantiate TrustLevel3Requirements.new(@user) once and read its public current/min/max methods. Emit rolling-period metrics, all-time metrics, likes, flags, and the blocking conditions required by the spec:

- days_visited, num_topics_replied_to, topics_viewed, posts_read;
- topics_viewed_all_time, posts_read_all_time;
- num_likes_given, num_likes_received, num_likes_received_days, num_likes_received_users;
- num_flagged_posts and num_flagged_by_users with at_most;
- not_silenced, not_suspended, and no_recent_penalties in blocking_conditions.

Use the object’s calculated time_period, min_*, max_*, and penalty_counts. Do not recreate dynamic caps or percentages in the plugin.

- [ ] **Step 6: Add the route and localized labels**

Add level_progress to UsersController:

~~~ruby
def level_progress
  user = User.find_by(id: params[:id])
  return head :not_found if user.blank?

  render json: ConnectIdentity::LevelProgressSerializer.new(user).to_h
end
~~~

Require the serializer in plugin.rb and append:

~~~ruby
get "/connect/identity/users/:id/level-progress" => "connect_identity/users#level_progress"
~~~

Add zh-CN labels for every emitted key and the three display groups. Use English fallback strings in I18n.t calls so a missing site locale cannot produce a missing-label response.

- [ ] **Step 7: Update plugin documentation and run the specs**

Document that the endpoint is Portal-only, HMAC-protected, returns only current/next level progress, and leaves the old status endpoint unchanged. Run:

~~~powershell
bundle exec rspec plugins/connect-identity/spec/requests/connect_identity_spec.rb
~~~

Expected: PASS for old and new request examples.

- [ ] **Step 8: Commit the Discourse endpoint**

~~~powershell
git add -- discourse-plugin/connect-identity
git commit -m "feat: expose discourse level progress"
~~~

### Task 4: Load optional progress on the Portal homepage

**Files:**
- Modify: internal/apps/http.go
- Modify: internal/apps/http_test.go
- Modify: cmd/connect/main.go

**Interfaces:**
- Consumes identity.LevelProgressLookup from Task 2.
- Produces LevelProgress in the homepage render data map, typed as *identity.LevelProgressSnapshot.
- Existing HTTPDependencies callers remain valid when LevelProgress is nil.

- [ ] **Step 1: Write failing handler tests**

Add a fake lookup with a call counter:

~~~go
type fakeLevelProgressLookup struct {
    snapshot identity.LevelProgressSnapshot
    err      error
    calls    int
}

func (f *fakeLevelProgressLookup) CurrentLevelProgress(context.Context, string) (identity.LevelProgressSnapshot, error) {
    f.calls++
    return f.snapshot, f.err
}
~~~

Test that GET /connect/apps calls it once and renders successfully; a lookup error still returns HTTP 200 and includes the future sync-fallback marker; and GET /connect/apps/app_1 does not call it.

- [ ] **Step 2: Run focused handler tests to verify they fail**

~~~powershell
go test ./internal/apps -run 'TestHTTPHandler(Loads|Degrades|DoesNotLoad).*LevelProgress' -count=1
~~~

Expected: FAIL because HTTPDependencies has no progress lookup and the homepage has no fallback marker.

- [ ] **Step 3: Add the optional dependency and homepage call**

Add LevelProgress identity.LevelProgressLookup to HTTPDependencies and levelProgress to HTTPHandler. In list, after ListMine succeeds and before rendering, use:

~~~go
var levelProgress *identity.LevelProgressSnapshot
if h.levelProgress != nil {
    if snapshot, err := h.levelProgress.CurrentLevelProgress(r.Context(), subject); err == nil {
        levelProgress = &snapshot
    }
}
~~~

Pass LevelProgress only in the homepage data map. Do not add this call to item, newForm, review, consent, OAuth, or error paths.

- [ ] **Step 4: Wire the dependency**

Add LevelProgress: levelProgress to the existing apps.HTTPDependencies literal in cmd/connect/main.go. Do not add it to review or OAuth dependencies.

- [ ] **Step 5: Run handler and application tests**

~~~powershell
go test ./internal/apps ./internal/web -count=1
~~~

Expected: PASS for the new tests and all existing app/template tests.

- [ ] **Step 6: Commit homepage data loading**

~~~powershell
git add -- internal/apps/http.go internal/apps/http_test.go cmd/connect/main.go
git commit -m "feat: load level progress on portal home"
~~~

### Task 5: Render the level progress panel without changing page structure

**Files:**
- Modify: internal/web/templates.go
- Modify: internal/web/templates/app-list.html
- Modify: internal/web/static/portal.css
- Modify: internal/web/templates_test.go

**Interfaces:**
- Consumes optional *identity.LevelProgressSnapshot from Task 4.
- Produces an accessible server-rendered panel with current level, next level, conditions, progress values, blocking status, and sync degradation.
- Preserves portal-shell, portal-sidebar, workspace-grid, the existing four app stats cards, app list, and onboarding rail.

- [ ] **Step 1: Write failing renderer tests**

Create a representative snapshot with current level 2, target level 3, one unmet activity requirement, one met interaction requirement, and one blocking condition. Render app-list and assert that the body contains:

~~~text
用户等级进度
当前等级
目标等级
达到目标等级的条件
访问天数
40 / 50
成员
常规
~~~

Add tests for manual mode displaying “目标等级 4 需管理员授予” without an empty progress bar; no next level displaying “当前已是最高等级”; nil progress displaying “等级条件暂时无法同步” while preserving workspace-grid, app-table, and onboarding-list; and a label containing <script> being escaped.

- [ ] **Step 2: Run renderer tests to verify they fail**

~~~powershell
go test ./internal/web -run 'TestRenderer(Shows|Escapes).*Level|TestRenderer.*level' -count=1
~~~

Expected: FAIL because the template has no level-progress panel or helper functions.

- [ ] **Step 3: Add grouping and display helpers**

In internal/web/templates.go, add these entries to the existing template.FuncMap:

~~~go
"levelRequirementGroups": levelRequirementGroups,
"levelGroupLabel":       levelGroupLabel,
"requirementOperator":   requirementOperator,
"requirementUnit":       requirementUnit,
~~~

Implement levelRequirementGroups([]identity.LevelRequirement) []levelRequirementGroup, preserving API order while grouping activity, interaction, and compliance; levelGroupLabel mapping to 活跃程度、互动参与、合规与账号状态 with an unknown-key fallback; requirementOperator mapping at_least to 至少 and at_most to 不超过; and requirementUnit mapping count to 项, days to 天, and minutes to 分钟.

- [ ] **Step 4: Insert the panel into the existing homepage branch**

Insert one section class level-progress-panel after the existing stats-grid-compact and before workspace-grid. Do not move or rename any existing wrapper.

The successful branch renders CurrentLevel ID/label, NextLevel ID/label when non-nil, grouped requirements, current / target text, a native progress element for at_least conditions, at_most text using 不超过, a text status for every Met value, blocking_conditions, and period/generated-time hints. Nil, manual, locked, and none branches show explanatory states instead of fabricated requirements.

Use this shape as the insertion point and preserve the existing workspace-grid immediately after it:

~~~html
<section class="panel level-progress-panel" aria-labelledby="level-progress-title">
  <div class="panel-head">
    <h2 id="level-progress-title">用户等级进度</h2>
    <span class="panel-hint">达到下一等级的条件</span>
  </div>
  {{if .LevelProgress}}
  <div class="level-progress-body">
    <div class="level-progress-summary">当前等级 / 目标等级</div>
    {{range levelRequirementGroups .LevelProgress.Requirements}}
    <section class="level-progress-group" aria-labelledby="level-group-{{.Key}}">
      <h3 id="level-group-{{.Key}}">{{levelGroupLabel .Key}}</h3>
      {{range .Requirements}}
      <div class="level-requirement">
        <span>{{.Label}}</span>
        <span>{{.Current}} / {{.Target}} {{requirementUnit .Unit}}</span>
      </div>
      {{end}}
    </section>
    {{end}}
  </div>
  {{else}}
  <div class="panel-body">等级条件暂时无法同步</div>
  {{end}}
</section>
~~~

- [ ] **Step 5: Add focused CSS without changing layout rules**

Add styles only for level-progress-* classes. Use existing tokens for surface, borders, colors, spacing, and radius. Style the summary as a two-column card area, conditions as a compact grid, progress tracks with visible fill, and add a max-width 720px rule that stacks the summary and condition rows.

Do not change portal-shell columns, portal-sidebar, workspace-grid columns, existing app statistics spacing, or navigation styles.

- [ ] **Step 6: Run template tests and inspect markup**

~~~powershell
go test ./internal/web -count=1
git diff --check
~~~

Expected: PASS with escaped labels and no whitespace errors.

- [ ] **Step 7: Commit the panel**

~~~powershell
git add -- internal/web/templates.go internal/web/templates/app-list.html internal/web/static/portal.css internal/web/templates_test.go
git commit -m "feat: show user level progress on homepage"
~~~

### Task 6: Complete contract documentation and verification

**Files:**
- Review: docs/superpowers/specs/2026-09-02-user-level-progress-homepage-design.md
- Review: docs/superpowers/plans/2026-09-02-user-level-progress-homepage.md
- Review: all files changed by Tasks 1–5

**Interfaces:**
- Consumes the completed Portal and Discourse implementation.
- Produces a verified feature with passing unit/request/template tests and a browser check of the authenticated homepage.

- [ ] **Step 1: Run all focused unit and plugin tests**

~~~powershell
go test ./internal/identity ./internal/apps ./internal/web ./cmd/connect -count=1
bundle exec rspec plugins/connect-identity/spec/requests/connect_identity_spec.rb
~~~

Expected: PASS for both commands.

- [ ] **Step 2: Run the complete Go verification set**

~~~powershell
go test ./...
go vet ./...
go build ./cmd/connect
git diff --check
~~~

Expected: all commands exit 0 and no new dependency is added.

- [ ] **Step 3: Verify the homepage with browser-harness**

Use the repository browser-harness workflow against a running Portal with an authenticated session. Check that existing top bar, sidebar, four app statistic cards, application list, and onboarding rail remain present; the new panel shows current level, next level, condition values, and text status; desktop/mobile widths do not overflow; manual/maximum-level responses show explanatory states; and simulated Discourse/Redis failure keeps the application list available with the sync fallback.

Do not use browser JavaScript to call the private Discourse endpoint; verify that the page is server-rendered.

- [ ] **Step 4: Review the diff for scope and data leakage**

~~~powershell
git status --short
git diff 07082fd..HEAD --stat
git diff 07082fd..HEAD -- discourse-plugin/connect-identity internal/identity internal/apps internal/web cmd/connect/main.go
~~~

Confirm only the new progress contract, cache, homepage panel, tests, and plugin documentation changed; .workbuddy/ remains untracked and no secret or raw forum record appears.

- [ ] **Step 5: Commit only if verification documentation changed**

No code commit is required for a clean verification run. If a documentation-only correction is needed after verification, use:

~~~powershell
git add -- docs/superpowers/specs/2026-09-02-user-level-progress-homepage-design.md docs/superpowers/plans/2026-09-02-user-level-progress-homepage.md discourse-plugin/connect-identity/README.md
git commit -m "docs: finalize level progress verification"
~~~
