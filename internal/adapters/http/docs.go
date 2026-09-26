package httpapi

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/m0ntbl4ck/voltia/api"
)

// docsPage loads Swagger UI from the embedded assets and points it at the spec.
const docsPage = `<!doctype html>
<html lang="es">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>VoltIA API</title>
  <link rel="stylesheet" href="/api/docs/assets/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="/api/docs/assets/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function () {
      SwaggerUIBundle({ url: "/api/openapi.yaml", dom_id: "#swagger-ui", persistAuthorization: true });
    };
  </script>
</body>
</html>
`

// docsRoutes serves the OpenAPI document and its viewer. They need no session:
// the spec describes the API and "Try it out" still needs the login cookie.
func docsRoutes(r chi.Router) {
	assets, err := fs.Sub(api.FS, "swagger-ui")
	if err != nil {
		panic(err) // the directory is embedded at build time
	}
	spec, err := api.FS.ReadFile("openapi.yaml")
	if err != nil {
		panic(err)
	}
	r.Get("/api/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(spec)
	})
	r.Get("/api/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsPage))
	})
	r.Handle("/api/docs/assets/*", http.StripPrefix("/api/docs/assets/", http.FileServerFS(assets)))
}
