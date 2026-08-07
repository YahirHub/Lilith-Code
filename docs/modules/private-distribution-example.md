# Plantilla de distribución privada

Este archivo es documentación; **no** debe copiarse como `.go` al repo público.
En el privado crea `internal/distribution/company.go`:

```go
//go:build company

package distribution

import (
    _ "github.com/lilith/li/modules/company/infra"
    _ "github.com/lilith/li/modules/company/jsecure"
)
```

Cada paquete importado se registra mediante `moduleapi.Register` en `init()`.
No es necesario editar `cmd/li`, `internal/tui/chat.go` ni
`internal/tui/commands.go` para incorporar un módulo nuevo.
