# Fecha
2026-08-05

# Objetivo
Hacer que la bóveda SSH se abra únicamente cuando una conexión necesite una credencial guardada, mostrar secretos y aprobaciones dentro del chat, distinguir claramente cada tipo de contraseña y convertir las conexiones SSH en sesiones lógicas estables que se recuperen automáticamente ante `EOF` o cortes de transporte. GitZip debe seguir funcionando sin confirmación genérica.

# Decisiones tomadas
- La bóveda no se desbloquea al iniciar Lilith.
- El primer `connect_server` o reparación de transporte que necesite una contraseña/passphrase cifrada abre un popup enmascarado dentro del chat para solicitar la contraseña maestra.
- Una vez abierta, la clave derivada y el contenido descifrado permanecen sólo en memoria hasta `lock_vault` o `sshremote.ShutdownAll()`; las tareas siguientes no vuelven a solicitar la contraseña maestra.
- Los textos diferencian explícitamente tres secretos: contraseña maestra de la bóveda, contraseña de la cuenta del servidor remoto y passphrase de la clave privada.
- Confirmaciones y secretos comparten el dock local del chat. Ningún valor secreto entra al transcript, historial, sesión, logs ni mensajes del modelo.
- Un `connection_id` identifica una conexión lógica estable. El `*ssh.Client`, SFTP y los canales son transportes reemplazables detrás de ese ID.
- Ante `EOF`, `broken pipe`, reset, cierre de red o ausencia de exit status, Lilith marca el transporte como inválido y reconecta automáticamente usando los mismos datos de autenticación. Las credenciales solicitadas por popup se conservan sólo en la conexión lógica en memoria para que una reparación no vuelva a pedirlas.
- Si el canal se pierde después de iniciar un comando, Lilith no repite el comando a ciegas para evitar efectos duplicados. Recupera la conexión, conserva el ID y devuelve `exit_status_known=false` con una instrucción estructurada para verificar el resultado.
- Las operaciones SFTP se reintentan después de reparar el transporte; las operaciones de estado final determinista recuperan su ejecución sin exigir una reconexión manual.
- La política predeterminada permanece en `critical_only`: comandos, lecturas y conexiones normales continúan sin interrupción; cambios de archivos, eliminaciones y perfiles/credenciales requieren aprobación.
- GitZip no hereda la aprobación genérica de SSH. La inclusión explícita de un `.env` real conserva su autorización independiente mediante `ProtectEnvFiles`.

# Arquitectura actual
- `RootModel.Init()` sólo inicia la pantalla activa y el consumidor de `interaction.Bridge`; ya no existe un comando de desbloqueo de bóveda durante el arranque.
- `RootModel.Update()` dirige solicitudes `interaction.Secret` y `interaction.Confirm` al `ChatModel`, conservando y restaurando la pantalla anterior cuando corresponde.
- `permission_widget.go` implementa un popup con borde dentro del pie del chat. Las confirmaciones SSH con alcance ofrecen `Permitir una vez`, `Permitir en esta sesión`, `Permitir siempre en este proyecto` y `Denegar`; los secretos usan `textinput` en modo contraseña, validación de longitud y confirmación en dos pasos cuando se crea una contraseña maestra.
- `CredentialVault.requireUnlocked` abre perezosamente una bóveda existente cuando la operación realmente necesita la credencial. `GetForConnection` incluye el nombre del servidor en la explicación del prompt.
- `Connection` conserva `connectOptions`, `Generation`, `ReconnectCount`, salud del transporte y último error. `reconnectMu` evita reparaciones simultáneas y reemplaza cliente, SFTP, agente y shells sin modificar `Connection.ID`. Un watcher sobre `ssh.Client.Wait()` detecta cierres aunque no haya una operación activa.
- `newSession`, preparación PTY, SFTP y shell reintentan tras reparar el transporte. `pwd` y `cd` pueden repetir una vez su consulta idempotente después de una reparación. El keepalive sólo marca un fallo; la siguiente acción lo repara sin abrir prompts inesperados en segundo plano.
- `Exec` normaliza `io.EOF` y `ssh.ExitMissingError`: recupera el transporte y devuelve un resultado exitoso estructuralmente, pero con estado de salida desconocido si el comando ya había empezado. El timeout predeterminado de `exec` remoto es ilimitado; sólo se aplica un límite cuando `timeout_seconds` se envía explícitamente.
- `Settings.SSHRemote` reemplaza el interruptor `SSHSafeMode` con presets y una matriz personalizada persistida en `settings.json`.
- `sshPermissionForAction` asigna las 35 acciones de `ssh_remote` a conexiones, lecturas, comandos, cambios de archivos, eliminaciones, credenciales o bóveda local.
- `/config > Seguridad > SSH Remoto` es una pantalla anidada exclusiva para la política SSH.

# Políticas disponibles
- `critical_only`: confirma cambios de archivos, eliminaciones y modificaciones de perfiles/credenciales.
- `every_action`: confirma todas las acciones SSH registradas.
- `commands_only`: confirma únicamente `exec`, apertura de shell y escritura en shell.
- `trust_model`: no solicita aprobaciones SSH; la contraseña maestra y la protección `.env` siguen separadas.
- `custom`: permite activar individualmente conexiones, lecturas/navegación, comandos/shells, cambios de archivos, eliminaciones, perfiles/credenciales y bloqueo manual de la bóveda.

# Compatibilidad y migración
- Un `settings.json` antiguo con `sshSafeMode=true` migra a `critical_only`.
- `sshSafeMode=false` migra a `trust_model`.
- Los campos personalizados se conservan al cambiar temporalmente a otro preset.
- Los IDs de conexión existentes siguen usando el formato `ssh-*`; el cambio sólo altera la recuperación del transporte interno.

# Archivos importantes modificados
- `cmd/li/main.go`
- `internal/config/config.go`
- `internal/config/ssh_security_test.go`
- `internal/sshremote/vault.go`
- `internal/sshremote/manager.go`
- `internal/sshremote/vault_test.go`
- `internal/sshremote/manager_test.go`
- `internal/tools/ssh_remote.go`
- `internal/tools/ssh_remote_test.go`
- `internal/tools/gitzip.go`
- `internal/tools/gitzip_test.go`
- `internal/tui/app.go`
- `internal/tui/chat.go`
- `internal/tui/secure_prompt.go`
- `internal/tui/permission_widget.go`
- `internal/tui/permission_widget_test.go`
- `internal/tui/config_screen.go`
- `internal/tui/ssh_security_config.go`
- `internal/tui/config_screen_test.go`

# Pruebas y validación
- Regresiones para desbloqueo perezoso y reutilización de la bóveda abierta.
- Verificación de que los prompts de creación/desbloqueo explican que se trata de la contraseña maestra y no de la contraseña del servidor.
- Pruebas del popup secreto dentro del chat, enmascaramiento y entrega mediante `interaction.Bridge`.
- Cobertura de detección de `EOF`, ausencia de exit status, reset de conexión, conservación de generación/ID lógico y reutilización en memoria de la contraseña remota solicitada por popup.
- Pruebas existentes para migración, presets, 35 categorías SSH, permisos por sesión/proyecto, GitZip remoto sin confirmación genérica y widget de aprobación.
- `gofmt`, parser de Go y `git diff --check` sobre el árbol completo.

# Limitación del entorno de validación
El entorno de trabajo dispone de Go 1.23.2, mientras el proyecto exige Go 1.25.12, y no tiene acceso DNS para descargar el toolchain ni los módulos SSH/TUI ausentes del caché. Por ello la suite completa y el build oficial deben ejecutarse en el equipo del usuario. La validación local se limita a formato, sintaxis, auditorías estáticas y las pruebas de paquetes que puedan resolverse sin descargas.

# Pruebas manuales recomendadas
1. Iniciar Lilith con una bóveda existente y comprobar que no aparece ningún prompt durante el arranque.
2. Ejecutar `connect_server` con una contraseña guardada y verificar que el popup dice “Contraseña maestra de la bóveda SSH” y aclara que no es la del servidor.
3. Ejecutar varias tareas SSH con el mismo ID y confirmar que la bóveda no vuelve a pedir la contraseña.
4. Conectar usando `prompt_password=true` y verificar que el popup dice “Contraseña del servidor remoto”.
5. Usar un servidor/proxy que cierre el transporte tras cada comando: el siguiente comando debe conservar el mismo `connection_id`, aumentar `connection_generation`, no volver a pedir la contraseña del servidor y no mostrar `error: EOF`.
6. Ejecutar un comando largo que pierda el canal y comprobar `transport_recovered=true` y `exit_status_known=false`; verificar su estado sin `close`/`connect_server`.
7. Probar operaciones SFTP después de un corte y comprobar la recuperación automática.
8. Probar cada preset de `/config > Seguridad > SSH Remoto`.
9. Usar GitZip local y remoto sin confirmación genérica y comprobar la autorización independiente de `.env`.

# Próximos pasos
- Ejecutar `test.cmd -Vet` y `go build -tags=grammar_set_core ./cmd/li` con Go 1.25.12.
- Validar contra el servidor de las capturas, especialmente el proxy que omite exit status o cierra el canal después de cada `exec`.
