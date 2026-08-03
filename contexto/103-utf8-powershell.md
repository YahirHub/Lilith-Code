# 103 — Captura UTF-8 correcta en PowerShell

## Problema verificado

La auditoría de `run_terminal_command` confirmó que selección de shell, códigos
de salida, separación stdout/stderr, timeout, logs largos y guard de heredoc
funcionaban. La excepción era Windows PowerShell: una línea con acentos y emoji
se recibía como `l�nea ??` cuando PowerShell era la shell neutral elegida en
Windows.

El backend convertía correctamente a `string` los bytes capturados por
`os/exec`; el daño ocurría antes. Windows PowerShell 5.1 puede usar la página de
códigos heredada al escribir en streams redirigidos, por lo que caracteres no
representables se degradan antes de llegar a Go.

## Decisión

Antes de ejecutar un comando PowerShell, Lilith antepone únicamente:

```powershell
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [Console]::OutputEncoding
```

Así PowerShell produce UTF-8 sin BOM para stdout/stderr y usa la misma
codificación al intercambiar texto con programas nativos.

No se aplica una recodificación heurística después de capturar la salida: una
vez que una página de códigos reemplaza un emoji por `?`, no es posible
reconstruirlo de manera fiable. La corrección actúa en el productor.

## Preservación de semántica

- El comando original sigue siendo el último fragmento ejecutado.
- No se agrega limpieza ni restauración después del comando, porque una sentencia
  posterior exitosa podría reemplazar el exit code observado por PowerShell.
- `Result.Command` conserva el comando solicitado y no expone el preámbulo
  interno.
- Bash, sh y CMD no reciben modificaciones.
- Timeout, cancelación del árbol de procesos, separación de streams y truncado
  mantienen el flujo anterior.

## Pruebas

Se añaden regresiones que validan:

- preámbulo UTF-8 sólo para PowerShell;
- comando original presente una sola vez y al final;
- Bash, sh y CMD sin alteraciones;
- en Windows, selección automática de PowerShell, stdout `línea 🚀`, stderr
  `error ágil 🧪` y exit code `3` en una misma ejecución.

El workflow manual ya ejecuta `go test -tags=grammar_set_core ./internal/tools
./internal/shell` en `windows-latest` mediante Windows PowerShell 5.1, por lo que
la prueba de integración se valida en el entorno afectado antes de publicar.

## Validación local realizada

El entorno de entrega sólo incluye Go 1.23.2 y no tiene red para descargar el
toolchain Go 1.24 definido por el proyecto. Para validar el paquete sin modificar
el entregable se usó temporalmente una copia de `go.mod` con directiva 1.23:

```text
ok github.com/lilith/li/internal/shell
```

También se compiló correctamente el binario de pruebas del paquete para
`GOOS=windows GOARCH=amd64 CGO_ENABLED=0`. La ejecución real de la prueba Unicode
Windows queda cubierta por el job `probar-instalador-windows` del workflow.
