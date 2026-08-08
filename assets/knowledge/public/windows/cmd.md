# CMD: operadores, variables, rutas y quoting

CMD (`cmd.exe`) no es PowerShell ni Bash. Confirma el shell antes de reutilizar sintaxis.

## Operadores

- `command1 & command2`: ejecuta ambos comandos.
- `command1 && command2`: ejecuta el segundo sólo si el primero termina correctamente.
- `command1 || command2`: ejecuta el segundo sólo si el primero falla.
- `command1 | command2`: conecta salida y entrada como texto.
- `&`, `|`, `<`, `>`, `(` y `)` son metacaracteres. Para pasarlos literalmente suele usarse `^`, sujeto al nivel de quoting de `cmd /c`.

## Variables y expansión

- Lee variables con `%NAME%` en una línea normal.
- Dentro de bloques parentizados, `%NAME%` se expande al parsear el bloque. Si el valor cambia dentro del bloque, activa delayed expansion con `setlocal EnableDelayedExpansion` y usa `!NAME!`.
- Define sin comillas accidentales: `set "NAME=value"`.
- En un archivo `.bat`, los parámetros son `%1`, `%2`; en la consola un bucle usa `%A`, mientras un batch usa `%%A`.

## Rutas y quoting

- Entrecomilla rutas con espacios: `cd /d "C:\My Project"`.
- `cd /d` cambia tanto la unidad como el directorio.
- La interacción entre las comillas externas de `cmd /c` y las comillas del ejecutable es especial. Para comandos complejos, prefiere un `.cmd` temporal controlado o pasa argumentos directamente desde el proceso anfitrión.
- No uses comillas simples como delimitador: CMD las trata como caracteres normales.

## Detección de errores

- `if errorlevel N` significa “código mayor o igual que N”. Para igualdad exacta usa `if %ERRORLEVEL% equ N`, con cuidado dentro de bloques.
- Convención habitual: `0` éxito, distinto de `0` error, pero cada ejecutable define su contrato.

Fuente oficial: Microsoft Learn, referencia de `cmd` y Windows Commands.
