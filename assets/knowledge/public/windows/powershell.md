# PowerShell en Windows: 5.1, 7, rutas y quoting

## Identificar el runtime antes de proponer sintaxis

- Windows PowerShell 5.1 se inicia normalmente con `powershell.exe`; corre sobre .NET Framework y viene integrado en muchas versiones de Windows.
- PowerShell 7+ se inicia con `pwsh`; corre sobre .NET moderno y puede convivir con 5.1.
- Comprueba la edición y versión con `$PSVersionTable.PSEdition` y `$PSVersionTable.PSVersion`.
- No presupongas que “PowerShell” significa 7. En automatización empresarial todavía es común 5.1.

## Encadenamiento correcto

- `&&` y `||` son operadores de cadena de pipelines introducidos en PowerShell 7. No son sintaxis portable a Windows PowerShell 5.1.
- En 5.1, ejecuta secuencialmente con `;` cuando el segundo comando siempre deba correr.
- Si el segundo comando depende del éxito del primero, usa una comprobación explícita:

```powershell
git status
if ($LASTEXITCODE -eq 0) { git diff --check }
```

- `$LASTEXITCODE` es relevante para ejecutables nativos. Para cmdlets, usa `try`/`catch` con `-ErrorAction Stop` cuando necesites convertir errores no terminantes en excepciones.

## Strings, variables y argumentos

- Comillas simples: contenido literal, sin expansión de variables: `'C:\$user\file.txt'`.
- Comillas dobles: expanden `$variables` y subexpresiones `$(...)`.
- El escape de PowerShell es el acento grave: `` ` ``. No copies escapes de Bash (`\`) ni de CMD (`^`).
- Para invocar una ruta ejecutable almacenada en una variable, usa el operador call:

```powershell
$tool = 'C:\Program Files\Tool\tool.exe'
& $tool --version
```

- Prefiere arrays de argumentos a construir un comando como string. Evita `Invoke-Expression` con datos externos.
- `--%` detiene el parsing de PowerShell para un comando nativo en Windows, pero también impide expansión posterior; úsalo sólo cuando el programa necesita una línea difícil de escapar.

## Rutas

- Entrecomilla rutas con espacios: `Get-Content -LiteralPath 'C:\My Project\file.txt'`.
- Usa `-LiteralPath` cuando una ruta puede contener `[` o `]`; `-Path` admite comodines.
- Combina segmentos con `Join-Path` en scripts que no deban asumir separadores.
- Las rutas UNC empiezan con `\\server\share`. No las conviertas en rutas POSIX.
- `~` y rutas relativas dependen de la ubicación actual; usa `Resolve-Path` sólo cuando el objetivo ya debe existir.

## Interoperabilidad

- `|` en PowerShell transmite objetos entre cmdlets; una pipeline de ejecutables nativos sigue transmitiendo texto/bytes según el host y la versión.
- Para ejecutar sintaxis propia de CMD, delimita explícitamente el intérprete: `cmd.exe /d /s /c "comando"`.
- Para ejecutar un script de PowerShell desde CMD, elige la edición de forma explícita: `powershell.exe -NoProfile -File script.ps1` o `pwsh -NoProfile -File script.ps1`.

Fuentes oficiales: Microsoft Learn, `about_Operators`, `about_Parsing`, `about_Quoting_Rules` y `Join-Path`.
