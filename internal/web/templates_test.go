package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/identity"
)

func TestRendererIncludesRoleAwarePortalNavigation(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-list", map[string]any{
		"Apps":   []domain.Application{},
		"Layout": Layout{Active: "apps", DisplayName: "Portal Admin", IsReviewer: true, IsAdmin: true},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	for _, label := range []string{"应用总览", "应用审核", "运营概览", "Portal Admin"} {
		if !strings.Contains(body, label) {
			t.Fatalf("navigation missing %q in %q", label, body)
		}
	}
	for _, marker := range []string{"portal-shell", "portal-sidebar", "workspace-grid", "app-table", "onboarding-list", "创建应用"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("dashboard layout missing %q", marker)
		}
	}
	if asideIndex, navIndex := strings.Index(body, `<aside class="portal-sidebar">`), strings.Index(body, `<nav class="portal-nav"`); asideIndex < 0 || navIndex < asideIndex {
		t.Fatalf("portal navigation must be rendered inside the sidebar: %q", body)
	}
	if !strings.Contains(body, AssetsPath+"/portal.") {
		t.Fatalf("stylesheet link is not content-addressed: %q", body)
	}
}

func TestRendererKeepsLevelNavigationAndHidesLevelProgressOnApplicationOverview(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	if err := renderer.Render(response, "app-list", map[string]any{
		"Apps":   []domain.Application{},
		"Layout": Layout{Active: "apps"},
	}); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	if !strings.Contains(body, `href="/">我的等级</a>`) {
		t.Fatalf("application overview must keep the level navigation link: %q", body)
	}
	if strings.Contains(body, `class="level-progress-panel"`) || strings.Contains(body, "用户等级进度") {
		t.Fatalf("application overview must not show the level progress panel: %q", body)
	}
}

func TestRendererUsesOneLinkForEachApplicationRow(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-list", map[string]any{
		"Apps": []domain.Application{{
			ID:          "app_1",
			Name:        "Example App",
			Description: "Example description",
			Status:      domain.StatusApproved,
		}},
		"Layout": Layout{Active: "apps"},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	if got := strings.Count(body, `href="/connect/apps/app_1"`); got != 1 {
		t.Fatalf("application row has %d detail links, want one full-row link", got)
	}
	if !strings.Contains(body, `class="app-row-link"`) ||
		!strings.Contains(body, `aria-label="查看 Example App 详情"`) {
		t.Fatalf("application row is missing its labelled full-row link: %q", body)
	}
}

func TestRendererShowsAutomaticLevelProgressOnHomepage(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "level-progress", levelProgressRenderData(&identity.LevelProgressSnapshot{
		CurrentLevel:  identity.LevelInfo{ID: 2, Key: "member", Label: "成员"},
		NextLevel:     &identity.LevelInfo{ID: 3, Key: "regular", Label: "常规"},
		PromotionMode: "automatic",
		Requirements: []identity.LevelRequirement{
			{Key: "days_visited", Label: "访问天数", Group: "activity", Scope: "rolling_period", PeriodDays: intPtr(100), Current: 40, Target: 50, Operator: "at_least", Unit: "days", Met: false},
			{Key: "likes_received", Label: "获得点赞", Group: "interaction", Scope: "rolling_period", PeriodDays: intPtr(100), Current: 35, Target: 30, Operator: "at_least", Unit: "count", Met: true},
		},
		BlockingConditions: []identity.LevelBlockingCondition{
			{Key: "not_silenced", Label: "账号未被禁言", Met: true},
		},
		GeneratedAt: time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC),
	}))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	for _, expected := range []string{
		"用户等级进度",
		"当前等级",
		"目标等级",
		"达到目标等级的条件",
		"访问天数",
		"40 / 50 天",
		"成员",
		"常规",
		"活跃程度",
		"互动参与",
		"账号未被禁言",
		"未达成",
		"已达成",
		"按最近 100 天数据计算",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("automatic level progress missing %q in %q", expected, body)
		}
	}
	if !strings.Contains(body, "<progress") {
		t.Fatalf("automatic level progress should use native progress markup: %q", body)
	}
	for _, expected := range []string{
		`id="level-requirement-days-visited-label"`,
		`<progress class="level-requirement-progress" aria-labelledby="level-requirement-days-visited-label"`,
		`id="level-requirement-likes-received-label"`,
		`<progress class="level-requirement-progress" aria-labelledby="level-requirement-likes-received-label"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("automatic level progress missing accessible progress markup %q in %q", expected, body)
		}
	}
}

func TestRendererUsesReferenceStyleMarkupForLevelProgressHome(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	data := levelProgressRenderData(&identity.LevelProgressSnapshot{
		CurrentLevel:  identity.LevelInfo{ID: 2, Key: "member", Label: "成员"},
		NextLevel:     &identity.LevelInfo{ID: 3, Key: "regular", Label: "常规"},
		PromotionMode: "automatic",
		RequirementsMet: func() *bool {
			value := false
			return &value
		}(),
		Requirements: []identity.LevelRequirement{
			{Key: "days_visited", Label: "访问天数", Group: "activity", Current: 40, Target: 50, Operator: "at_least", Unit: "days"},
		},
	})
	data["Layout"] = Layout{Active: "home", DisplayName: "Portal User", TrustLevel: 2}
	response := httptest.NewRecorder()
	if err := renderer.Render(response, "level-progress", data); err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	body := response.Body.String()
	for _, marker := range []string{
		`class="portal-body level-progress-page"`,
		`class="panel-head level-progress-panel-head"`,
		`class="panel-hint level-progress-panel-subtitle"`,
		`class="level-progress-badge`,
		`class="level-progress-ring level-progress-ring-current"`,
		`class="level-progress-ring level-progress-ring-target"`,
		`class="level-progress-ring-caption"`,
		`class="level-progress-ring level-progress-ring-mode"`,
		`class="level-progress-requirement-meter"`,
		`class="level-progress-requirement-track"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("reference-style homepage markup missing %q in %q", marker, body)
		}
	}
	for _, marker := range []string{"workspace-grid", "app-table", "onboarding-list"} {
		if strings.Contains(body, marker) {
			t.Fatalf("reference-style homepage unexpectedly contains app marker %q", marker)
		}
	}
}

func TestRendererShowsAtMostLevelRequirementAsText(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "level-progress", levelProgressRenderData(&identity.LevelProgressSnapshot{
		CurrentLevel:  identity.LevelInfo{ID: 2, Key: "member", Label: "成员"},
		NextLevel:     &identity.LevelInfo{ID: 3, Key: "regular", Label: "常规"},
		PromotionMode: "automatic",
		Requirements: []identity.LevelRequirement{
			{Key: "flagged_posts", Label: "被确认举报的帖子", Group: "compliance", Scope: "rolling_period", PeriodDays: intPtr(100), Current: 3, Target: 5, Operator: "at_most", Unit: "count", Met: true},
		},
		GeneratedAt: time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC),
	}))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	for _, expected := range []string{
		"被确认举报的帖子",
		"当前 3 项，不超过 5 项",
		"合规与账号状态",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("at_most level requirement missing %q in %q", expected, body)
		}
	}
	if strings.Contains(body, `<progress`) {
		t.Fatalf("at_most requirement should not render a progress bar: %q", body)
	}
}

func TestRendererShowsManualLevelProgressState(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "level-progress", levelProgressRenderData(&identity.LevelProgressSnapshot{
		CurrentLevel:  identity.LevelInfo{ID: 3, Key: "regular", Label: "常规"},
		NextLevel:     &identity.LevelInfo{ID: 4, Key: "leader", Label: "领导者"},
		PromotionMode: "manual",
		GeneratedAt:   time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC),
	}))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	if !strings.Contains(body, "目标等级 4 需管理员授予") {
		t.Fatalf("manual level progress message missing in %q", body)
	}
	if strings.Contains(body, `<progress`) {
		t.Fatalf("manual level progress should not render an empty progress bar: %q", body)
	}
}

func TestRendererShowsTerminalAndLockedLevelStates(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}

	maxResponse := httptest.NewRecorder()
	err = renderer.Render(maxResponse, "level-progress", levelProgressRenderData(&identity.LevelProgressSnapshot{
		CurrentLevel:  identity.LevelInfo{ID: 4, Key: "leader", Label: "领导者"},
		PromotionMode: "none",
		GeneratedAt:   time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC),
	}))
	if err != nil {
		t.Fatalf("Render(max) error = %v", err)
	}
	if !strings.Contains(maxResponse.Body.String(), "当前已是最高等级") {
		t.Fatalf("terminal level progress message missing in %q", maxResponse.Body.String())
	}

	lockedResponse := httptest.NewRecorder()
	err = renderer.Render(lockedResponse, "level-progress", levelProgressRenderData(&identity.LevelProgressSnapshot{
		CurrentLevel:  identity.LevelInfo{ID: 2, Key: "member", Label: "成员"},
		NextLevel:     &identity.LevelInfo{ID: 3, Key: "regular", Label: "常规"},
		PromotionMode: "locked",
		BlockingConditions: []identity.LevelBlockingCondition{
			{Key: "manual_locked", Label: "当前等级已被锁定", Met: false},
		},
		GeneratedAt: time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC),
	}))
	if err != nil {
		t.Fatalf("Render(locked) error = %v", err)
	}
	lockedBody := lockedResponse.Body.String()
	for _, expected := range []string{"当前等级已被锁定", "暂不能自动升级"} {
		if !strings.Contains(lockedBody, expected) {
			t.Fatalf("locked level progress missing %q in %q", expected, lockedBody)
		}
	}
}

func TestRendererShowsNilLevelProgressFallbackOnHomepage(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "level-progress", levelProgressRenderData(nil))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	for _, expected := range []string{"当前等级", ">2<", "等级条件暂时无法同步"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("nil level progress is missing %q in %q", expected, body)
		}
	}
	if strings.Contains(body, `id="level-progress-target-title"`) {
		t.Fatalf("nil level progress should not render a target-level summary card: %q", body)
	}
}

func TestRendererEscapesLevelProgressLabels(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "level-progress", levelProgressRenderData(&identity.LevelProgressSnapshot{
		CurrentLevel:  identity.LevelInfo{ID: 2, Key: "member", Label: "成员"},
		NextLevel:     &identity.LevelInfo{ID: 3, Key: "regular", Label: "常规"},
		PromotionMode: "automatic",
		Requirements: []identity.LevelRequirement{
			{Key: "scripted", Label: `<script>alert(1)</script>`, Group: "activity", Scope: "rolling_period", PeriodDays: intPtr(30), Current: 1, Target: 2, Operator: "at_least", Unit: "count", Met: false},
		},
		GeneratedAt: time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC),
	}))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	if strings.Contains(body, `<script>alert(1)</script>`) || !strings.Contains(body, `&lt;script&gt;alert(1)&lt;/script&gt;`) {
		t.Fatalf("level progress labels are not escaped: %q", body)
	}
}

func TestRendererShowsLocalizedFallbackForBlankRequirementGroup(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "level-progress", levelProgressRenderData(&identity.LevelProgressSnapshot{
		CurrentLevel:  identity.LevelInfo{ID: 2, Key: "member", Label: "成员"},
		NextLevel:     &identity.LevelInfo{ID: 3, Key: "regular", Label: "常规"},
		PromotionMode: "automatic",
		Requirements: []identity.LevelRequirement{
			{Key: "custom_metric", Label: "自定义条件", Group: "", Scope: "account_lifetime", Current: 1, Target: 2, Operator: "at_least", Unit: "count", Met: false},
		},
		GeneratedAt: time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC),
	}))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	if !strings.Contains(body, "其他条件") {
		t.Fatalf("blank requirement group should use localized fallback in %q", body)
	}
	if strings.Contains(body, ">other<") {
		t.Fatalf("blank requirement group should not expose raw fallback key in %q", body)
	}
}

func TestRendererScopesConsentFormActionToValidatedCallbackOrigins(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	redirectURIs := []string{
		"https://mail.example/auth/callback",
		"https://MAIL.EXAMPLE/other",
		"https://portal.example:8443/callback",
		"http://insecure.example/callback",
		"https://localhost/callback",
		"https://127.0.0.1/callback",
		"https://user:pass@evil.example/callback",
		"https://evil.example/callback?next=1",
		"https://evil.example/callback#fragment",
		"https://*.evil.example/callback",
	}
	consent := httptest.NewRecorder()
	if err := renderer.RenderWithFormAction(consent, "consent", map[string]any{
		"Client": map[string]any{"Name": "Example"}, "Scopes": []string{"openid"},
		"Action": "/connect/consent", "CSRFToken": "csrf",
	}, redirectURIs); err != nil {
		t.Fatalf("RenderWithFormAction(consent) error = %v", err)
	}
	want := "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self' https://mail.example https://portal.example:8443"
	if csp := consent.Header().Get("Content-Security-Policy"); csp != want {
		t.Fatalf("consent CSP = %q, want %q", csp, want)
	}

	errorPage := httptest.NewRecorder()
	if err := renderer.RenderWithFormAction(errorPage, "error", map[string]any{
		"Title": "Error", "Message": "Denied", "Back": "/",
	}, redirectURIs); err != nil {
		t.Fatalf("RenderWithFormAction(error) error = %v", err)
	}
	if csp := errorPage.Header().Get("Content-Security-Policy"); strings.Contains(csp, "mail.example") || strings.Contains(csp, "portal.example") {
		t.Fatalf("non-consent CSP leaked callback origins: %q", csp)
	}
}

func TestRendererShowsProvisioningActionForExistingDraft(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-list", map[string]any{
		"PageTitle": "应用详情",
		"Apps":      []domain.Application{{ID: "app_1", Name: "Draft", Status: domain.StatusDraft}},
		"Layout":    Layout{Active: "apps", CSRFToken: "csrf"},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	if !strings.Contains(body, `action="/connect/apps/app_1/submit"`) || !strings.Contains(body, "开始上线") {
		t.Fatalf("draft provisioning action missing: %q", body)
	}
	for _, marker := range []string{"detail-layout", "action-rail", `href="/connect/apps/app_1/edit"`} {
		if !strings.Contains(body, marker) {
			t.Fatalf("draft detail layout missing %q", marker)
		}
	}
}

func TestRendererShowsCredentialActionsForApprovedApplication(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-list", map[string]any{
		"PageTitle":   "应用详情",
		"ConfirmedAt": int64(1787918400),
		"Apps":        []domain.Application{{ID: "app_1", Name: "Approved", Status: domain.StatusApproved, ClientID: "client_1"}},
		"Layout":      Layout{Active: "apps", CSRFToken: "csrf"},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	for _, expected := range []string{
		`href="/connect/apps/app_1/edit"`,
		`action="/connect/apps/app_1/secret"`,
		`action="/connect/apps/app_1/rotate-secret"`,
		`action="/connect/apps/app_1/delete"`,
		`name="confirmed_at" value="1787918400"`,
		"查看 Client Secret", "轮换 Secret", "删除应用",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("approved credential action missing %q: %s", expected, body)
		}
	}
}

func TestRendererGroupsApprovedDangerActionsInTheActionRail(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-list", map[string]any{
		"PageTitle":   "应用详情",
		"ConfirmedAt": int64(1787918400),
		"Apps":        []domain.Application{{ID: "app_1", Name: "Approved", Status: domain.StatusApproved, ClientID: "client_1"}},
		"Layout":      Layout{Active: "apps", CSRFToken: "csrf"},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	if !strings.Contains(body, `<section class="panel danger-panel" aria-labelledby="danger-actions-title">`) {
		t.Fatalf("approved detail is missing the unified danger action panel: %q", body)
	}
	if !strings.Contains(body, `class="danger-actions"`) || strings.Count(body, `class="danger-action"`) != 2 {
		t.Fatalf("approved detail should render rotate and delete as two aligned danger actions: %q", body)
	}
	for _, className := range []string{"danger-action-summary", "danger-action-body"} {
		if !strings.Contains(body, `class="`+className+`"`) {
			t.Fatalf("approved detail is missing .%s: %q", className, body)
		}
	}
}

func TestPortalTemplatesUseInternalDocsLinks(t *testing.T) {
	for _, filename := range []string{
		"app-form.html", "app-list.html", "admin-overview.html",
		"layout.html", "review-queue.html", "secret.html",
	} {
		body, err := templateFS.ReadFile("templates/" + filename)
		if err != nil {
			t.Fatalf("read %s: %v", filename, err)
		}
		content := string(body)
		if strings.Contains(content, "github.com/puremixai/xai-connect") {
			t.Fatalf("%s still points documentation to GitHub", filename)
		}
		if !strings.Contains(content, `href="/docs`) {
			t.Fatalf("%s is missing an internal /docs link", filename)
		}
	}
}

func TestRendererEscapesApplicationContentAndSetsSecurityHeaders(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-form", map[string]any{
		"Title": "Create", "Name": "<script>alert(1)</script>", "Layout": Layout{},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.Contains(response.Body.String(), "<script>alert(1)</script>") ||
		!strings.Contains(response.Body.String(), "&lt;script&gt;") {
		t.Fatalf("rendered body is not escaped: %q", response.Body.String())
	}
	if response.Header().Get("Content-Security-Policy") == "" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		response.Header().Get("Referrer-Policy") == "" {
		t.Fatalf("security headers = %#v", response.Header())
	}
}

func TestRendererRejectsUnknownTemplate(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	if err := renderer.Render(httptest.NewRecorder(), "missing", nil); err == nil {
		t.Fatal("Render() accepted unknown template")
	}
}

func TestRendererCanSetNonSuccessStatusBeforeRendering(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	if err := renderer.RenderStatus(response, 403, "error", map[string]any{
		"Title": "Forbidden", "Message": "Denied", "Back": "/",
	}); err != nil {
		t.Fatalf("RenderStatus() error = %v", err)
	}
	if response.Code != 403 {
		t.Fatalf("status = %d", response.Code)
	}
}

func levelProgressRenderData(levelProgress *identity.LevelProgressSnapshot) map[string]any {
	return map[string]any{
		"Layout":        Layout{Active: "home", DisplayName: "Portal User", TrustLevel: 2},
		"LevelProgress": levelProgress,
	}
}

func intPtr(value int) *int {
	return &value
}
