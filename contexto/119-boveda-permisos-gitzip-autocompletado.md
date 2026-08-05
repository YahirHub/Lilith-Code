# Fecha
2026-08-05

# Objetivo
Corregir la ambigüedad entre la contraseña maestra de la bóveda y la contraseña del servidor, garantizar que una bóveda abierta se reutilice durante toda la ejecución, ampliar el alcance de las aprobaciones SSH, hacer GitZip seleccionable por rutas y mejorar el orden/autocompletado visual de comandos y skills.

# Problemas observados
- El popup infería el tipo de secreto buscando palabras en el título y el mensaje. Una explicación de contraseña remota que mencionaba «bóveda» terminaba rotulando el campo como «Contraseña maestra de la bóveda SSH».
- El usuario podía interpretar dos solicitudes de contraseña remota como dos desbloqueos de bóveda debido a ese rótulo incorrecto.
- Las aprobaciones SSH sólo ofrecían permitir una vez o denegar.
- GitZip no exponía una selección explícita de rutas incluidas y omitidas mediante su contrato público.
- La paleta slash conservaba el orden original para coincidencias difusas: `/reload-plugins` podía aparecer antes que la coincidencia exacta `/login`.
- `Tab` completaba una skill sin espacio final y el token de skill no se diferenciaba visualmente del texto normal.

# Decisiones técnicas
- `interaction.Request` incluye `SecretKind`. La UI no deduce secretos a partir de texto libre.
- Tipos soportados: `vault_master`, `remote_password`, `key_passphrase` y `generic`.
- `CredentialVault.EnsureWritable` abre o crea la bóveda antes de pedir la credencial que se guardará. `Set` vuelve a validar el estado, pero no genera otro prompt si la bóveda ya está abierta.
- La bóveda conserva clave y payload descifrado sólo en memoria hasta `lock_vault`, `ShutdownAll` o cierre del CLI. Llamadas concurrentes se serializan por el mutex de la bóveda; la segunda reutiliza el estado abierto.
- El widget SSH agrega decisiones `allow_once`, `allow_session`, `allow_project` y `deny`.
- Los permisos de sesión se almacenan sólo en `interaction.Bridge`. Los permisos de proyecto se persisten en `Settings.SSHProjectApprovals`, acotados por proyecto, acción y destino lógico.
- `/config > Seguridad > SSH Remoto` permite borrar todos los permisos permanentes del proyecto actual sin alterar el preset o la matriz de categorías.
- GitZip admite `include_paths` y `exclude_paths`; `extra_excludes` permanece como alias compatible. `source_path` es la raíz exacta de la carpeta que se empaqueta.
- El selector GitZip usa rutas relativas a la raíz y globs `*`, `**` y `?`. Seleccionar una carpeta incluye sus descendientes; los directorios no seleccionados siguen recorriéndose cuando contienen una ruta incluida más profunda.
- La selección se aplica tanto al escaneo local como al manifiesto remoto. Nunca se incluyen `.git`, el archivo de salida, manifiestos temporales o `.env` protegidos sin autorización.
- La paleta slash usa ranking: exacta, prefijo, subcadena y subsecuencia con penalización por huecos. Los comandos ganan empates frente a skills.
- Las filas de skill tienen un tipo explícito y usan el color secundario. `Tab` completa `/<nombre> ` con espacio final.
- El textarea permite resaltar un prefijo sin modificar su valor, cursor, wrapping ni historial. El chat mantiene una caché de nombres de skills actualizada al construir la paleta para evitar escanear SKILL.md en cada frame.

# Archivos principales
- `internal/interaction/bridge.go`
- `internal/sshremote/vault.go`
- `internal/sshremote/manager.go`
- `internal/tools/ssh_remote.go`
- `internal/config/config.go`
- `internal/tui/permission_widget.go`
- `internal/tui/ssh_security_config.go`
- `internal/gitzip/gitzip.go`
- `internal/tools/gitzip.go`
- `internal/tui/commands.go`
- `internal/tui/suggestion_menu.go`
- `internal/tui/chat.go`
- `internal/tui/uikit/textarea/textarea.go`

# Pruebas agregadas o actualizadas
- La contraseña remota conserva su etiqueta aunque el mensaje mencione la bóveda.
- Una bóveda abierta permite guardar varias credenciales sin repetir la contraseña maestra.
- Una aprobación de sesión evita una segunda solicitud con la misma clave de capacidad.
- Los permisos permanentes se deduplican, cuentan y eliminan por proyecto.
- El widget con alcance muestra las cuatro decisiones.
- GitZip incluye sólo las rutas seleccionadas, respeta exclusiones adicionales y trata `**/` como cero o más directorios.
- `/login` queda primero ante una consulta exacta.
- `Tab` completa una skill con un espacio final.
- Las filas y el token inicial de skill usan el estilo secundario.

# Validación del entorno
- `gofmt` y `git diff --check` se ejecutan sobre el árbol completo.
- Pasan `go test` y `go vet` para `./internal/interaction`, `./internal/config` y `./internal/gitzip` usando temporalmente el Go local 1.23; el `go.mod` final permanece en Go 1.25.12.
- Pasan de forma aislada las pruebas de `vault.go` con un stub temporal de `scrypt` y las pruebas del textarea con un stub temporal de `uniseg`; esos stubs no forman parte del proyecto entregado.
- La suite SSH/TUI completa no puede ejecutarse en este entorno porque faltan módulos en caché y el DNS no permite descargarlos. Debe validarse en Windows con `test.cmd -Vet` y `go build -tags=grammar_set_core ./cmd/li`.

# Pruebas manuales recomendadas
1. Ejecutar `set_server_password` con la bóveda cerrada: primero debe aparecer «Contraseña maestra de la bóveda SSH» y después «Contraseña del servidor remoto».
2. Guardar otra contraseña en la misma ejecución: sólo debe aparecer «Contraseña del servidor remoto».
3. Conectar con `prompt_password=true`: el título y el campo deben decir «Contraseña del servidor remoto», aunque la explicación mencione la bóveda.
4. Elegir «Permitir en esta sesión» y repetir la misma acción/destino: no debe aparecer otro widget durante esa ejecución.
5. Elegir «Permitir siempre en este proyecto», reiniciar Lilith y repetir la misma acción/destino: debe continuar sin popup. Después borrar los permisos desde `/config > Seguridad > SSH Remoto` y comprobar que vuelve a preguntar.
6. Crear un GitZip con `source_path`, `include_paths` y `exclude_paths`, revisar el manifiesto local y repetir con `remote_create`.
7. Escribir `/login` y comprobar que es el primer resultado.
8. Escribir un fragmento de una skill, pulsar `Tab` y continuar escribiendo: debe existir un espacio entre el nombre y las instrucciones, y la skill debe verse en color secundario.
