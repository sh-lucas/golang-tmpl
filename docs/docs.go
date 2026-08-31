// Package docs embeds the generated OpenAPI specification.
package docs

import _ "embed"

// SwaggerJSON is the generated OpenAPI document.
//
//go:embed swagger.json
var SwaggerJSON []byte
