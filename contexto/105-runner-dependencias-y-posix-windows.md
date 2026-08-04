# 105 — Runner de dependencias y sintaxis POSIX en Windows

> Nota posterior: la estrategia `go mod download all` fue reemplazada por la validación documentada en `108-integridad-modulos-y-tests-windows.md`, después de comprobar que descargaba módulos ajenos y no corregía un `go.sum` incompleto en el repositorio.

## Fallos observados

El job Windows del workflow manual fallaba por dos causas independientes:

1. `github.com/odvcencio/gotreesitter v0.48.0` estaba declarado en `go.mod`,
   pero su entrada de contenido todavía no existía en `go.sum`. Con carga
   perezosa de módulos, `go mod download` sin argumentos puede descargar sólo
   el `go.mod` de una dependencia y no el ZIP completo que necesita el paquete
   importado. Después, `go test` en modo readonly rechazaba la dependencia.
2. El detector de shell reconocía `VAR=value comando`, pero no
   `VAR=value; comando`. En Windows el comando de regresión
   `VALUE=native-bash-ok; printf "$VALUE"` se clasificaba como neutral y se
   enviaba a PowerShell 7, donde la asignación POSIX producía un error no
   terminante y el proceso acababa con código 0.

## Corrección del workflow

Los dos jobs del workflow `Publicar release` ahora ejecutan antes de probar:

```text
go mod download all
go mod verify
```

`all` obliga a materializar el contenido completo del grafo seleccionado y
registra los checksums de los ZIP descargados en el workspace del runner. Luego
las pruebas usan `-mod=readonly`, por lo que ninguna dependencia adicional puede
aparecer silenciosamente durante `go test`.

La misma preparación se aplica al job Linux de release para evitar que el fallo
sólo se desplace al segundo job.

## Corrección del detector POSIX

La expresión de sintaxis POSIX acepta ahora una asignación de entorno cuando el
valor queda seguido por cualquiera de estas terminaciones:

- espacio y el comando que recibe la variable;
- punto y coma;
- salto de línea;
- fin de la entrada.

Se conservan las prioridades anteriores: PowerShell se detecta primero, después
CMD y finalmente POSIX. Un comando neutral en Windows continúa usando
PowerShell.

## Cobertura

`TestChooseShellKindDetectsWindowsSyntax` añade regresiones para:

- `VALUE=native-bash-ok; printf "$VALUE"`;
- asignación y comando en líneas separadas;
- una asignación POSIX aislada al final de la entrada.

La prueba de integración Windows existente vuelve a cubrir el comando exacto
que falló en GitHub Actions y exige Bash más stdout `native-bash-ok`.

## Validación local

- Las pruebas unitarias de selección de shell pasan con Go 1.23 en una copia
  temporal de la directiva del módulo, sin modificar el entregable.
- La suite completa de `internal/shell`, su variante `-race` y `go vet` pasan
  en Linux; las pruebas Windows compilan como ejecutable PE amd64 y se ejecutarán
  realmente en `windows-latest`.
- Se reprodujo con un proxy Go local que `go mod download` sólo añadía el hash
  `/go.mod`, mientras `go mod download all` añadía también el hash del módulo y
  permitía `go test -mod=readonly`.
- `go.mod` y la versión del proyecto no cambian.
