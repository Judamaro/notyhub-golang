package api

import (
	_ "embed"
	"net/http"
	"strings"

	"github.com/Judamaro/notyhub-golang/internal/config"
)

//go:embed openapi.yaml
var openAPITemplate string

const swaggerUIVersion = "5.17.14"

const swaggerHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>NotyHub API</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@` + swaggerUIVersion + `/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@` + swaggerUIVersion + `/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({ url: "/openapi/v1.yaml", dom_id: "#swagger-ui" });
  </script>
</body>
</html>`

// mapOpenAPI serves the OpenAPI document and Swagger UI (development only).
func mapOpenAPI(mux *http.ServeMux, cfg config.Config) {
	document := strings.NewReplacer(
		"{{API_KEY_HEADER}}", cfg.APIKeys.HeaderName,
		"{{HUB_PATH}}", cfg.Realtime.HubPath,
	).Replace(openAPITemplate)

	mux.HandleFunc("GET /openapi/v1.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write([]byte(document))
	})
	mux.HandleFunc("GET /swagger", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(swaggerHTML))
	})
}
