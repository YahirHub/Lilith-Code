# 137. `/init` one-shot, Knowledge lazy y ciclo Goal → Build

## Objetivo

Implementar tres capacidades relacionadas sin mezclarlas conceptualmente:

1. `/init` acepta instrucciones adicionales válidas sólo para esa ejecución;
2. Knowledge aporta referencias locales consultables y lazy, separadas de Skills;
3. Goal captura estado durable, Build ejecuta y una acción explícita completa.

Se conserva la decisión anterior de no imponer presupuestos artificiales de
tokens, pasos, turnos o tiempo a Goal.

## `/init [instrucciones adicionales]`

`core.project` reenvía los argumentos mediante la capacidad opcional nueva
`ProjectInitializerWithInstructions`, conservando `ProjectInitializer` como
fallback compatible para hosts que sólo admiten `/init` sin argumentos. La TUI ejecuta la
inicialización normal y añade el texto dentro de
`<additional_init_instructions>`.

El bloque aclara tres invariantes:

- la instrucción puede pedir trabajo relacionado además de `LILITH.md`;
- no crea ni sustituye Goal;
- no se conserva como regla permanente separada.

La forma visible del comando incluye los argumentos para que el transcript sea
auditable. `/init` materializa también las tools Knowledge, de modo que puede
consultar convenciones de Lilith sin incorporar toda la documentación al prompt.

## Knowledge independiente y lazy

Los documentos públicos viven bajo:

```text
assets/knowledge/public/**
```

`assets.KnowledgeFS()` expone el árbol embebido y `internal/knowledge` mantiene
una base de sólo lectura. Crear `knowledge.Base` no recorre ni normaliza los
documentos. El índice se construye exactamente una vez con la primera llamada a
`Search` o `Topics`; `Read` abre directamente un path canónico y devuelve un
rango acotado.

Tools nuevas:

- `knowledge_search(query, namespace?, topic?, limit?)`;
- `knowledge_read(path, offset?, limit?)`;
- `knowledge_topics(namespace?)`.

No forman parte del set normal de schemas por detectar una palabra como Docker o
PowerShell. Permanecen detrás de `tool_search`, lo que mantiene cero contenido
Knowledge en el contexto hasta que el modelo decide consultarlo. El prompt
estable sí exige no improvisar sintaxis incierta de plataforma, versión,
quoting o herramienta y descubrir Knowledge cuando haga falta.

La misma `Base` entra en `tools.Env` del chat y se hereda en subagentes. Knowledge
no activa workflows ni sustituye `assets/skills`; una Skill puede cargar una
referencia transversal sólo al necesitar datos concretos. El procedimiento,
decisiones, seguridad y ejemplos propios de un dominio permanecen en su Skill.
Si ésta ya contiene módulos de referencia, Knowledge no mantiene una guía
paralela. Por eso Git/GitHub y Docker/Compose conservan una única fuente de
verdad en sus Skills dedicadas. `ponytail-development` sólo documenta el handoff
general y no copia contenido Knowledge.

## Namespaces privados

La build pública reserva `public`. Un paquete enlazado estáticamente por una
distribución downstream puede embeber su propio `fs.FS` y registrar, durante
`init`, por ejemplo:

```go
knowledge.MustRegisterNamespace("company", docs)
```

El registro rechaza namespaces inválidos, duplicados y cualquier intento de
reemplazar `public`. El paquete privado se importa desde el archivo build-tagged
de `internal/distribution`, igual que los módulos empresariales; el core público
no necesita `if company` ni conoce los runbooks privados.

## Contenido inicial

El namespace público incluye referencias acotadas para:

- Windows PowerShell 5.1 frente a PowerShell 7, CMD, rutas y quoting;
- Linux: shell, permisos, filesystem y procesos;
- Termux: `$PREFIX`, paquetes, storage Android, procesos y límites;
- Android Debug Bridge por USB, Wi-Fi con pairing y TCP/IP heredado, selección
  de dispositivos, comandos frecuentes, diagnóstico y seguridad;
- arquitectura de Lilith: módulos, Skills, agentes, tools, distribuciones,
  Knowledge privada y desarrollo/validación.

Git/GitHub y Docker/Compose se excluyen deliberadamente de Knowledge porque las
Skills `git-github` y `docker-development` ya poseen referencias modulares. La
separación evita dos fuentes de verdad, aunque ambos runtimes sean lazy.

En particular, la referencia Windows no mezcla intérpretes: `&&`/`||` son
pipeline chain operators de PowerShell 7 y no sintaxis válida de Windows
PowerShell 5.1; CMD mantiene sus propios operadores y reglas de escape.

## Semántica Goal → Build

Goal continúa siendo un modo de entrada, no un runtime de implementación. Tab y
Shift+Tab alternan Goal ↔ Build; Plan conserva `/plan` y Tab vuelve de Plan a
Build. Al enviar texto desde Goal:

1. sustituye el objetivo durable y lo deja `active`;
2. el selector vuelve inmediatamente a Build;
3. la continuación comienza con herramientas/modo Build.

Los mensajes Build ordinarios no sustituyen Goal. Los contadores siguen siendo
diagnósticos y no detienen la ejecución.

`goal_complete(summary)` es la única acción model-facing para completar: exige
un resumen final y lo persiste en `goal.summary`. `update_goal` queda limitado a
`active`/`blocked`. La TUI muestra el resumen al cerrar el loop.

## Interrupción y reanudación

Se añade el estado durable `interrupted`. Un Goal activo pasa a ese estado ante:

- cierre de Lilith;
- carga de una sesión que quedó guardada como activa;
- error definitivo del proveedor o fallo fatal de tools;
- imposibilidad de iniciar la continuación por no haber proveedor/modelo.

La zona de interacción muestra `GOAL INTERRUMPIDO` con la acción clicable
`Continuar`; no reanuda automáticamente al abrir la sesión.

`/resume` (aliases `/continue`, `/continuar`) reabre `paused`, `blocked`,
`interrupted` o `complete` sin cambiar objetivo, creación ni uso. En Build, los
textos exactos `continue`, `continuar`, `resume` o `reanudar` activan el mismo
control local si el Goal es reanudable: nunca llegan como un mensaje literal al
provider.

## Compatibilidad y pruebas

- Los estados heredados `budget_limited`/`usage_limited` siguen migrándose a
  `active`.
- `UpdateStatus(complete)` se conserva para control manual/compatibilidad
  interna, pero el schema del modelo sólo ofrece `goal_complete`.
- El catálogo público suma `/resume` y queda en 25 comandos exactos.
- Tests cubren indexado/búsqueda/lectura/topics —incluido ADB y la ausencia de
  topics duplicados para Git/Docker—, namespace privado y traversal;
  tool discovery lazy; `/init` one-shot; resumen y resume de Goal; toggle de
  modos; frase local de continuación; banner; ownership modular.

Validado con Go 1.25.12:

```text
go test -mod=readonly -tags=grammar_set_core ./...
go test -race -mod=readonly -tags=grammar_set_core ./...
go vet -mod=readonly -tags=grammar_set_core ./...
CGO_ENABLED=0 go build -mod=readonly -tags=grammar_set_core ./cmd/li
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -tags=grammar_set_core ./cmd/li
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -exec=true -mod=readonly -tags=grammar_set_core ./...
```

El test Android usa `-exec=true` para compilar toda la suite arm64 desde el host
Linux sin intentar ejecutar binarios Android localmente.
