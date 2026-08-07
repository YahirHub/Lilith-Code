# Lilith modules

Este árbol contiene las capacidades modulares compiladas dentro de Lilith.
Desde v0.3.2 los slash commands generales ya no se inyectan desde un
`core.commands` monolítico: cada capacidad pública vive bajo `modules/core/**`
y se registra mediante `moduleapi.Register`.

```text
modules/
├── core/                 # módulos públicos oficiales
│   ├── help/
│   ├── mode/
│   ├── rewind/
│   ├── skills/
│   ├── providers/
│   └── ...
└── company/              # reservado para downstream privado; no existe en main
```

Para una distribución privada añade paquetes bajo `modules/company/**` y un
archivo `internal/distribution/company.go` protegido con `//go:build company`
que los importe por side effect. El archivo público
`internal/distribution/builtin.go` no debe modificarse.

Los módulos dependen de `internal/moduleapi`, nunca de `internal/tui`. Las
operaciones que requieren la TUI se solicitan mediante capacidades opcionales
del host. Esto mantiene el código empresarial aislado de refactors internos y
reduce conflictos al fusionar el `main` público.

Consulta `docs/modules/README.md`.
