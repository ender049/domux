package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesEmbeddedIndex(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"Domux", "应用", "域名", "节点"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected dashboard HTML to contain %q", want)
		}
	}
}

func TestStaticUIKeepsProductInformationArchitecture(t *testing.T) {
	t.Parallel()

	content, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	body := string(content)
	for _, want := range []string{
		`id="system-overview"`,
		`role="tablist"`,
		`aria-label="统计导航"`,
		`id="workspace-applications"`,
		`id="workspace-domains"`,
		`id="workspace-nodes"`,
		`id="workspace-logs"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("index.html should contain product IA marker %q", want)
		}
	}
	for _, banned := range []string{"Config", "Routes", "Playground", "Servers", "Settings"} {
		if strings.Contains(body, ">"+banned+"<") {
			t.Fatalf("index.html should not expose godoxy navigation item %q", banned)
		}
	}
}

func TestStaticUIUsesCompactObjectConsoleLayout(t *testing.T) {
	t.Parallel()

	content, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	body := string(content)
	for _, want := range []string{
		"topbar-inner",
		"app-topbar",
		"summary-strip",
		"object-grid",
		"page-toolbar",
		"modal-panel",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("index.html should contain godoxy-inspired layout marker %q", want)
		}
	}
	for _, banned := range []string{"hero-panel", "content-split", "detail-panel", "segmented-tabs", "最近任务", "最近下发", "清空新建"} {
		if strings.Contains(body, banned) {
			t.Fatalf("index.html should not contain over-designed layout marker %q", banned)
		}
	}
}

func TestStaticUIHighlightsUserStoriesInCode(t *testing.T) {
	t.Parallel()

	content, err := fs.ReadFile(staticFS, "app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	body := string(content)
	for _, want := range []string{
		"已代理",
		"未代理",
		"Docker 发现",
		"节点发现",
		"自定义",
		"节点",
		"代理",
		"同步",
		"通配",
		"zone-certificate-deploy-targets",
		"manager-table",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("app.js should contain user-story copy %q", want)
		}
	}
	for _, banned := range []string{"可以访问", "入口还没准备好", "当前服务器", "异常应用", "custom-app-delete", "zone-delete", "provider-new", "provider-delete", "source-delete", "agent-delete", "deploy-target-new", "deploy-target-delete", "zone-cert-targets", "zone-manage-targets", "zone-certificate-summary", "renderApplicationDetail", "renderJobs", "renderDeployRuns"} {
		if strings.Contains(body, banned) {
			t.Fatalf("app.js should not contain obsolete UI pattern %q", banned)
		}
	}
}

func TestStaticUIFetchesExistingAPIResources(t *testing.T) {
	t.Parallel()

	content, err := fs.ReadFile(staticFS, "app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	body := string(content)
	for _, endpoint := range []string{
		"/api/v1/applications",
		"/api/v1/apps",
		"/api/v1/zones",
		"/api/v1/ddns-providers",
		"/api/v1/runtimes",
		"/api/v1/agents",
		"/api/v1/ddns",
		"/api/v1/deploy-targets",
		"/api/v1/certificates",
		"/api/v1/deployments",
		"/api/v1/jobs",
	} {
		if !strings.Contains(body, endpoint) {
			t.Fatalf("app.js should fetch %q", endpoint)
		}
	}
}

func TestStaticUIUsesAccessibleResponsiveBuildingBlocks(t *testing.T) {
	t.Parallel()

	indexContent, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	styleContent, err := fs.ReadFile(staticFS, "styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	index := string(indexContent)
	styles := string(styleContent)
	for _, want := range []string{
		`<a class="skip-link"`,
		`aria-label="统计导航"`,
		`aria-live="polite"`,
		`aria-label="应用分类"`,
		`<dialog id="custom-app-dialog"`,
		`multi-picker`,
	} {
		if !strings.Contains(index, want) {
			t.Fatalf("index.html should contain accessibility marker %q", want)
		}
	}
	for _, want := range []string{
		"@media (max-width: 1120px)",
		"@media (max-width: 760px)",
		"@media (prefers-reduced-motion: reduce)",
		":focus-visible",
		"min-height: 40px",
	} {
		if !strings.Contains(styles, want) {
			t.Fatalf("styles.css should contain responsive/accessibility marker %q", want)
		}
	}
}
