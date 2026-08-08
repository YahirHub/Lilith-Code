# Módulos estáticos de Lilith

Lilith permite extender comandos slash desde paquetes Go enlazados al mismo
binario. No usa `plugin.so`, DLL, JavaScript ni procesos auxiliares: una build
con módulos sigue siendo un único ejecutable compatible con `CGO_ENABLED=0`.

La intención principal es mantener dos líneas de desarrollo sin convertir el
repo público en una colección de `if company { ... }`:

```text
repo público / main
  └─ core + módulos generales

repo privado / company
  ├─ merge periódico de public/main
  └─ modules/company/** + internal/distribution/company.go
```

## Contrato

La API estable está en `internal/moduleapi`. Un módulo registra una
`moduleapi.Definition` con:

- `ID` global, por ejemplo `company.deploy`;
- versión propia del módulo;
- `API: moduleapi.APIVersion`;
- dependencias obligatorias/opcionales por ID;
- comandos slash exactos (con `Order` opcional; los módulos privados sin orden se muestran después del core);
- rutas dinámicas delimitadas por `:` como `/skill:<nombre>`; el registry rechaza prefijos dinámicos sin `:` para impedir que intercepten accidentalmente un comando exacto del core.

Un módulo recibe `moduleapi.Host`, no `*tui.ChatModel`. La interfaz base se
mantiene deliberadamente pequeña: proyecto/configuración, mensajes y diagnóstico
de módulos. Funciones opcionales se exponen por capacidades separadas como
`moduleapi.SkillInvoker`, `moduleapi.Submitter`, `moduleapi.ScreenOpener` y
`moduleapi.RewindState`. Así una versión futura puede añadir capacidades sin
agrandar `Host` y sin obligar a recompilar/corregir todos los módulos privados
por un cambio de interfaz.

Un módulo que necesite una capacidad opcional debe comprobarla de forma segura:

```go
invoker, ok := host.(moduleapi.SkillInvoker)
if !ok {
    host.AddError("Esta distribución no expone Agent Skills.")
    return
}
invoker.InvokeSkill("frontend-development", args)
```

Esto evita que un refactor normal de la TUI obligue a resolver conflictos en
cada módulo empresarial.

El registry es fail-closed:

- una API incompatible deshabilita el módulo;
- una dependencia obligatoria ausente lo deshabilita;
- un ID duplicado lo deshabilita;
- un comando/alias/ruta que colisiona con otro módulo no sobrescribe nada;
- un comando exacto no puede instalarse dentro del namespace de una ruta dinámica de otro módulo (ni viceversa);
- `core.*` está reservado para módulos con source `builtin`/`core`;
- `/modules` muestra el motivo del módulo deshabilitado.

## Crear un módulo privado

En el repo privado crea, por ejemplo:

```text
modules/company/hello/
└── module.go
```

```go
package hello

import "github.com/lilith/li/internal/moduleapi"

func init() {
    moduleapi.Register(moduleapi.Definition{
        ID:          "company.hello",
        Name:        "Company Hello",
        Version:     "1",
        Description: "Comandos internos de ejemplo.",
        Source:      "company",
        API:         moduleapi.APIVersion,
        Commands: []moduleapi.Command{{
            Name:        "company-hello",
            Description: "Comprueba que la distribución privada está activa.",
            Handler: func(host moduleapi.Host, args string) {
                host.AddSystem("company module OK · project=" + host.ProjectRoot())
            },
        }},
    })
}
```

Después crea **sólo en el repo privado**:

```text
internal/distribution/company.go
```

```go
//go:build company

package distribution

import (
    _ "github.com/lilith/li/modules/company/hello"
    // _ "github.com/lilith/li/modules/company/jsecure"
    // _ "github.com/lilith/li/modules/company/deploy"
)
```

El archivo público `internal/distribution/builtin.go` no se modifica. Por eso un
merge de `main` no necesita tocar el punto de registro empresarial.

## Compilar

Distribución pública:

```bash
go run ./cmd/build build
```

Distribución privada:

```bash
go run ./cmd/build build --distribution company
```

El segundo comando compila con:

```text
-tags=grammar_set_core,company
CGO_ENABLED=0
```

Para desarrollo/pruebas del repo privado:

```bash
go test -mod=readonly -tags=grammar_set_core,company ./...
go run -tags=grammar_set_core,company ./cmd/li
```

## Flujo recomendado entre repos

En el repo privado conviene conservar **dos branches**: `main` como espejo del
público y `company` como distribución empresarial. Configura un remote
`upstream` apuntando al repo público:

```bash
git remote add upstream <repo-publico>
git fetch upstream

# Actualizar el espejo general sin mezclar código de empresa.
git switch main
git merge --ff-only upstream/main

# Llevar los cambios generales a la distribución privada.
git switch company
git merge main
```

Así `main` del privado sigue siendo comparable con el público y todos los
commits privados permanecen encima/en paralelo dentro de `company`.

Mantén los cambios empresariales preferentemente en:

```text
modules/company/**
internal/distribution/company.go
assets/skills/company-*/**       # si necesitas skills privadas embebidas
assets/agents/company-*.md       # si necesitas agentes privados embebidos
modules/company/knowledge/**     # namespace Knowledge privado registrado por init
```

Las carpetas nuevas bajo `assets` también suelen producir merges limpios porque
el repo público no necesita conocerlas.

## Knowledge privada y lazy

Knowledge es una base de referencias de sólo lectura, separada de Agent Skills.
El core público embebe `assets/knowledge/public/**` y construye su índice sólo al
primer `knowledge_search` o `knowledge_topics`; `knowledge_read` abre directamente
un documento acotado.

Una distribución privada puede embeber sus runbooks en un paquete de
`modules/company/**`, obtener un `fs.FS` con paths relativos y registrarlo en
`init` mediante `knowledge.MustRegisterNamespace("company", docs)`. Después debe
importar ese paquete desde el archivo build-tagged de distribución. El registry
rechaza namespaces inválidos, duplicados y el reemplazo de `public`. Las rutas
que ve el modelo quedan delimitadas como `company/runbooks/deploy.md`.

Una Skill puede consultar hechos transversales de plataforma en Knowledge, pero
la frontera se define por propiedad, no por tamaño: workflow, decisiones,
seguridad y ejemplos del dominio pertenecen a la Skill. Si esa Skill ya incluye
módulos de referencia —como Git/GitHub o Docker/Compose— no se crea un topic
Knowledge paralelo. Esto evita dos fuentes de verdad aunque ambas sean lazy.

## Módulos públicos incorporados

El core público usa la misma arquitectura que una distribución privada. No existe
ya un mega-módulo `core.commands`; las capacidades slash se registran desde
`modules/core/**`:

- `core.help`: `/help`;
- `core.project`: `/init`;
- `core.goal`: `/goal`, `/resume` (aliases `/continue`, `/continuar`);
- `core.mode`: `/plan`, `/build`;
- `core.compaction`: `/compact`;
- `core.rewind`: `/rewind`;
- `core.fork`: `/fork`;
- `core.memory`: `/memory`;
- `core.mcp`: `/mcp`;
- `core.agents`: `/tasks`, `/subtask`, `/agents`;
- `core.plugins`: `/plugins`, `/reload-plugins`;
- `core.providers`: `/login`, `/providers`, `/models`;
- `core.config`: `/config`;
- `core.session`: `/clear`, `/history`, `/exit`;
- `core.shell`: `/bash`;
- `core.skills`: `/skills:*`, `/skill:*`;
- `core.modules`: `/modules [id]`.

La TUI sólo materializa el `Registry` y adapta `moduleapi.Host`; no mantiene una
segunda tabla de comandos. `/modules` es la primera herramienta de diagnóstico
cuando una distribución privada compila pero un comando no aparece.

## Qué no hacer

No añadas comprobaciones `if company` dispersas en `chat.go`, `commands.go` o el
provider. No importes `internal/tui` desde módulos empresariales. No uses
`plugin.so`/DLL para esta arquitectura: perderías portabilidad y harías el build
estático mucho más frágil.

## CI del repo privado sin tocar el workflow público

Para minimizar conflictos, no edites `.github/workflows/release.yml` del repo
público en tu rama empresarial. Añade un workflow nuevo, por ejemplo
`.github/workflows/company-release.yml`, y usa los tags privados sólo allí:

```yaml
- name: Tests empresa
  run: go test -mod=readonly -tags=grammar_set_core,company ./...

- name: Build empresa
  run: go run ./cmd/build build --distribution company
```

De esta forma los cambios futuros del workflow público pueden entrar por merge
sin competir con el archivo de release empresarial.
