// Package api embeds the OpenAPI document and the Swagger UI that renders it,
// so the single binary serves its own documentation without network access.
package api

import "embed"

// FS holds openapi.yaml and the Swagger UI assets under swagger-ui/.
//
//go:embed openapi.yaml swagger-ui/swagger-ui-bundle.js swagger-ui/swagger-ui.css swagger-ui/LICENSE
var FS embed.FS
