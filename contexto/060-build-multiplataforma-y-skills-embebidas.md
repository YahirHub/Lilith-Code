# 060 · Build multiplataforma y skills embebidas

Fecha: 2026-07-28

## Objetivo

Añadir un builder Go autocontenido para generar los binarios finales de `li` en
Linux/Windows y permitir que Lilith distribuya Agent Skills propias dentro del
mismo binario, sin obligar al usuario a copiar esas skills a `~/.li`.

## Builder

`cmd/build/main.go` conserva las operaciones históricas de toolchain y añade el
build multiplataforma como acción predeterminada:

```text
go run ./cmd/build             build multiplataforma
go run ./cmd/build build       build multiplataforma
go run ./cmd/build check       revisar toolchain externa
go run ./cmd/build install     instalar toolchain externa
go run ./cmd/build install -f  reinstalar toolchain externa
```

Targets generados en `dist/`:

```text
li-linux-amd64
li-linux-arm64
li-linux-armv7
li-windows-amd64.exe
li-windows-arm64.exe
```

Cada compilación usa:

- `CGO_ENABLED=0`;
- `-trimpath`;
- `-buildvcs=false`;
- `-ldflags="-s -w ..."`;
- `GOOS`, `GOARCH` y `GOARM` controlados por el builder;
- `version` y `commit` inyectados desde Git cuando está disponible.

El builder elimina del entorno variables de cross-compilación heredadas que
podrían contaminar un target (`GOOS`, `GOARCH`, `GOARM`, `GOAMD64`, etc.).

## Skills embebidas

Se añade la estructura:

```text
assets/
├── embed.go
└── skills/
    ├── README.md
    └── <skill>/
        ├── SKILL.md
        └── references/...
```

`assets/embed.go` usa `go:embed`, por lo que todo lo colocado debajo de
`assets/skills` queda incorporado en cada binario `li` durante la compilación.

El runtime actual de skills trabaja sobre rutas de filesystem para mantener una
única implementación de `skill_read`, `skill_search` y `skill_files`. Por ello,
al usarse por primera vez, las skills embebidas se materializan en una caché
privada y content-addressed bajo:

```text
~/.li/.cache/bundled-skills/<hash>/
```

La copia usa directorios `0700` y archivos `0600`. El hash depende del contenido
embebido, de modo que una nueva compilación con skills distintas utiliza una
caché distinta. El proceso sólo prepara esta caché una vez por directorio de
configuración.

## Precedencia

El loader procesa fuentes de menor a mayor prioridad:

```text
assets/skills embebidas
        ↓
~/.agents/skills y ~/.claude/skills
        ↓
~/.li/skills
        ↓
<proyecto>/.agents/skills y .claude/skills
        ↓
<proyecto>/.li/skills
```

La deduplicación sigue usando el campo `name` del frontmatter. Por tanto, una
skill llamada `foo` en `~/.li/skills/foo/SKILL.md` reemplaza automáticamente la
skill `foo` incorporada en el binario. Una skill del proyecto vuelve a tener
precedencia sobre ambas.

Las skills embebidas usan `Source: builtin`; `list_skills` acepta también el
filtro `source=builtin`.

## Herramientas

No existe una segunda implementación para skills internas. Después de
materializarlas, entran al mismo catálogo que las externas y funcionan con:

- `list_skills`;
- `skill_read`;
- `skill_search`;
- `skill_files`;
- `/skill:<nombre>` y `/skills:<nombre>`;
- activación automática mediante `<available_skills>`.

Aunque el diseño está pensado principalmente para `SKILL.md` y referencias
Markdown, el mecanismo preserva cualquier recurso incluido debajo de la skill.

## Validación

Se añadieron pruebas para:

- materialización de los assets embebidos;
- precedencia `builtin < user < project`;
- acción predeterminada `build`;
- compatibilidad de los subcomandos `install/check`;
- limpieza de variables de cross-compilación heredadas.

El entorno disponible sólo tiene Go 1.23.2 mientras el proyecto declara Go
1.24.0. Para validar el código nuevo sin modificar el proyecto se realizó una
copia temporal con la directiva `go` bajada únicamente en esa copia y se
obtuvieron correctamente:

```text
go test ./internal/skills ./cmd/build
```

La suite completa no puede ejecutarse offline porque las dependencias Charm,
Cobra y `x/text` no están presentes en la caché del entorno.
