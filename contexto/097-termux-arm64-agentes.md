# Termux ARM64 y agentes de portabilidad

## Estado vigente

Lilith conserva soporte de runtime para Termux ARM64 y dos subagentes embebidos:

- `termux-specialist`: implementación de compatibilidad Android/Termux.
- `termux-auditor`: auditoría de sólo lectura de paths, shell, teclado,
  subprocessos, storage y dependencias.

No se incluyen skills internas para instalar, compilar, actualizar o publicar
Lilith. Esas operaciones pertenecen a los scripts y documentación del
repositorio; no deben contaminar el catálogo de skills que usa el modelo para
trabajar en proyectos del usuario.

## Reglas de runtime

- Tratar Termux como Android y usar `$PREFIX`, `$HOME`, `PATH` y `pkg`.
- No asumir `/bin/sh`, `/usr/local/bin`, `sudo`, systemd, glibc ni root.
- Resolver shells y comandos por `PATH`.
- Mantener Enter, Ctrl+C, Esc, pegado, resize y cancelación de subprocessos.
- Separar compatibilidad verificada en dispositivo de validación sólo por build.
