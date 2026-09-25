package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDocsNeedNoSession(t *testing.T) {
	h := newServer(&fakeStarter{}, &fakeRuns{})

	page := doAnon(h, http.MethodGet, "/api/docs")
	if page.Code != http.StatusOK || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("docs page: %d %q", page.Code, page.Header().Get("Content-Type"))
	}
	for _, want := range []string{"/api/openapi.yaml", "/api/docs/assets/swagger-ui-bundle.js", "/api/docs/assets/swagger-ui.css"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("the page does not reference %s", want)
		}
	}

	spec := doAnon(h, http.MethodGet, "/api/openapi.yaml")
	if spec.Code != http.StatusOK || !strings.HasPrefix(spec.Header().Get("Content-Type"), "application/yaml") {
		t.Fatalf("spec: %d %q", spec.Code, spec.Header().Get("Content-Type"))
	}
	var doc struct {
		OpenAPI string `yaml:"openapi"`
		Info    struct {
			Title string `yaml:"title"`
		} `yaml:"info"`
	}
	if err := yaml.Unmarshal(spec.Body.Bytes(), &doc); err != nil || doc.OpenAPI != "3.0.3" || doc.Info.Title != "Voltia API" {
		t.Errorf("the served document is not the spec: %+v (%v)", doc, err)
	}
}

func TestSwaggerAssetsAreServedFromTheBinary(t *testing.T) {
	h := newServer(&fakeStarter{}, &fakeRuns{})
	tests := map[string]string{
		"/api/docs/assets/swagger-ui-bundle.js": "javascript",
		"/api/docs/assets/swagger-ui.css":       "css",
		"/api/docs/assets/LICENSE":              "",
	}
	for path, kind := range tests {
		rec := doAnon(h, http.MethodGet, path)
		if rec.Code != http.StatusOK || rec.Body.Len() < 1000 {
			t.Errorf("%s: status %d, %d bytes", path, rec.Code, rec.Body.Len())
		}
		if kind != "" && !strings.Contains(rec.Header().Get("Content-Type"), kind) {
			t.Errorf("%s: content type %q", path, rec.Header().Get("Content-Type"))
		}
	}
	if rec := doAnon(h, http.MethodGet, "/api/docs/assets/nope.js"); rec.Code != http.StatusNotFound {
		t.Errorf("a missing asset gave %d", rec.Code)
	}
}
