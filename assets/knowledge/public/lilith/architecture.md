# Lilith: módulos, skills, agentes, tools y distribuciones

## Capas distintas

- `modules/**`: capacidades estáticas del producto y comandos slash. Se registran con `internal/moduleapi`; no importan `internal/tui`.
- `assets/skills/**`: Agent Skills. Describen workflows reutilizables, pueden tener referencias/scripts/assets y se cargan progresivamente.
- `assets/agents/**`: definiciones de subagentes Claude-compatible que trabajan en contextos aislados.
- `internal/tools/**`: contratos de tool calling y selección lazy. Las tools reciben un `tools.Env` acotado.
- `assets/knowledge/**`: hechos/referencias consultables de sólo lectura. Knowledge no activa un workflow y no sustituye Skills.
- `internal/distribution/**`: punto de enlace estático de módulos públicos y privados.

## Distribución pública y privada

- El binario público enlaza `modules/core/**` desde `internal/distribution/builtin.go`.
- Una distribución privada añade un archivo build-tagged en `internal/distribution`, importa `modules/company/**` y compila con el tag de distribución además de `grammar_set_core`.
- Evita `if company` dispersos y plugins dinámicos `.so`/DLL. El objetivo es un binario único con `CGO_ENABLED=0`.
- Los módulos privados consumen `moduleapi.Host` y capacidades opcionales; las colisiones/versiones incompatibles fallan cerradas.

## Knowledge privada

Un paquete privado puede embeber Markdown y registrar un namespace sin modificar el core:

```go
//go:build company

package companyknowledge

import (
    "embed"
    "io/fs"
    "github.com/lilith/li/internal/knowledge"
)

//go:embed docs
var files embed.FS

func init() {
    docs, err := fs.Sub(files, "docs")
    if err != nil { panic(err) }
    knowledge.MustRegisterNamespace("company", docs)
}
```

El paquete debe quedar enlazado por la distribución privada. Los resultados usan rutas canónicas como `company/runbooks/deploy.md`.

## Skills que consultan Knowledge

Una skill puede indicar en sus instrucciones qué referencia transversal buscar
y luego llamar `knowledge_search`/`knowledge_read`. El límite es la propiedad
del contenido:

- el procedimiento, decisiones, seguridad y ejemplos propios de un dominio
  pertenecen a su Skill;
- los hechos de plataforma reutilizables que no activan un workflow pertenecen
  a Knowledge;
- si una Skill ya mantiene módulos de referencia para el dominio, Knowledge no
  debe crear una segunda guía con el mismo alcance.

Por eso Git/GitHub y Docker/Compose viven sólo en sus Skills dedicadas. ADB,
PowerShell, CMD, Linux y Termux permanecen en Knowledge como referencias de
plataforma disponibles para distintos workflows.
