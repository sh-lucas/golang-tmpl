// Package swagger embeds the generated OpenAPI specification.
package swagger

import _ "embed"

// SwaggerJSON is the generated OpenAPI document.
//
//go:embed swagger.json
var SwaggerJSON []byte
