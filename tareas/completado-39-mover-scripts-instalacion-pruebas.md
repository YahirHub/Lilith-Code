# Mover scripts de instalación y pruebas

## Objetivo

Reducir el ruido del directorio raíz trasladando los instaladores y helpers de
pruebas multiplataforma a `scripts/`, sin romper sus rutas relativas, descargas
directas, smoke tests ni el workflow de publicación.

## Alcance

- Mover `install.sh`, `install.ps1`, `install.cmd`, `test.ps1` y `test.cmd`.
- Actualizar URLs públicas, documentación, atributos EOL, CI y pruebas de los
  instaladores.
- Mantener `install.md` en el root como entrada documental de instalación.
- Verificar sintaxis, smoke tests, suite Go y builds multiplataforma.

## Estado

Completado. Pasaron sintaxis y simulaciones de instaladores, generación de notas
de release, manifiestos Go, suite normal/race, vet y builds Linux, Windows y
Android. La ejecución nativa de `scripts\test.cmd` queda para un host Windows.
