# Fecha
2026-07-29

# Objetivo
Corregir la resolución de modelo de los subagentes para que `model: default` use el proveedor/modelo elegido por el usuario en `/models`, mantener el comportamiento de modelos explícitos y permitir preferencias de varios modelos separadas por comas.

# Decisiones tomadas
- `default` se trata como herencia explícita del proveedor/modelo del turno padre.
- `inherit` conserva la semántica compatible existente.
- La precedencia sigue siendo: `CLAUDE_CODE_SUBAGENT_MODEL` cuando aplica, override `model` de la llamada `Agent`, `model:` del archivo y, finalmente, modelo del padre.
- Una lista `model: a, b, default` se interpreta como preferencias ordenadas y usa el primer candidato resoluble contra los providers/modelos configurados.
- Los aliases Claude `sonnet`, `opus`, `haiku` y `fable` conservan su resolución por nombre/ID. Si ninguno existe, el fallback compatible al modelo padre sólo ocurre después de probar todos los candidatos de la lista.
- No se cambia la selección global de `/models`: el subagente hereda el snapshot del modelo del turno padre, manteniendo la regla existente de que un cambio de modelo durante un turno aplica al siguiente turno.
- No se implementó reintento automático a otro candidato después de que un proveedor ya haya aceptado la selección local pero responda con un error remoto (por ejemplo, un 403 de plan). La lista resuelve disponibilidad en el catálogo configurado; para priorizar siempre la selección del usuario debe colocarse `default` primero o usar sólo `default`.

# Arquitectura actual
`internal/tui/plan_mode.go` entrega al runtime de subagentes `ParentProviderID` y `ParentModelID` tomados del turno activo. `internal/subagents/runtime.go` resuelve la preferencia del agente antes de crear la sesión hija y persiste el provider/model final en la sesión del subagente.

La resolución ahora acepta:

```md
model: default
model: inherit
model: gpt-5.4
model: commandcode/gpt-5.4
model: claude-sonnet-4-5, gpt-5.4, default
```

# Librerías usadas
No se agregaron dependencias. El cambio usa únicamente `strings`, `errors`, `fmt` y las estructuras de providers ya existentes.

# Archivos importantes modificados
- `internal/subagents/runtime.go`
- `internal/subagents/runtime_test.go`
- `internal/agents/agents_test.go`
- `internal/tools/agent.go`
- `assets/agents/README.md`
- `contexto/067-subagentes-compatibles-claude.md`
- `tareas/completado-13-modelos-subagentes-default-lista.md`

# Problemas encontrados
El agente `context7-docs` mostrado en la captura usaba un modelo Haiku explícito. El runtime daba prioridad al `model:` del agente frente al modelo del padre y resolvía el alias `haiku` contra los modelos configurados. Por ello acababa solicitando `claude-haiku-4-5-20251001` a CommandCode aunque el usuario tuviera otro modelo seleccionado en `/models`; CommandCode devolvía `HTTP 403 MODEL_NOT_IN_PLAN`.

El archivo concreto de `context7-docs` no está incluido en el ZIP recibido, por lo que no se modificó esa definición externa. Con este runtime el archivo puede usar `model: default` para heredar la selección del usuario.

# Soluciones implementadas
- Se agregó `default` como alias explícito del modelo padre.
- Se agregó parser de preferencias separado por comas y resolución en orden.
- Se mantuvo el soporte de modelos concretos, `provider/model` y aliases Claude.
- Se actualizó la descripción de la tool `Agent` para que el modelo conozca la nueva sintaxis.
- Se añadieron pruebas de regresión para `default`, modelo explícito, listas, fallback a `default`, override por invocación y parseo del frontmatter con comas.

# Validación
- `gofmt` aplicado a los Go modificados.
- `internal/agents` se copió a un laboratorio stdlib-only y `go test ./internal/agents` pasó con Go 1.23.2; `go vet ./internal/agents` también pasó.
- La implementación exacta de las funciones de resolución de `internal/subagents/runtime.go` se extrajo a un laboratorio mínimo junto con `providers/types.go`; `go test` y `go vet` pasaron para los casos `default`, explícito y lista.
- `go test ./internal/subagents`, `go test ./...` y `go vet ./...` se intentaron sobre el proyecto real, pero el entorno sólo tiene Go 1.23.2 mientras `go.mod` exige Go 1.24.0. La descarga automática de toolchain/dependencias no pudo completarse porque el sandbox no tiene salida DNS hacia `proxy.golang.org`.

# Pendientes
- Ejecutar en Windows o en un entorno con Go 1.24+ y dependencias disponibles:
  - `go test ./internal/agents ./internal/subagents ./internal/tools`
  - `go test ./...`
  - `go vet ./...`
- Cambiar el agente externo `context7-docs` a `model: default` si se quiere que siempre use el modelo seleccionado en `/models`.

# Próximos pasos
Validar el comportamiento real con un agente que tenga `model: default` y otro con una lista como `model: modelo-no-configurado, default`, comprobando en el panel del subagente que se muestra exactamente el modelo activo del turno padre.
