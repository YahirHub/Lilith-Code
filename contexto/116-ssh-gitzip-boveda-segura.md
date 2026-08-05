# Fecha
2026-08-05

# Objetivo
Adaptar a Lilith las herramientas GitZip y SSH/bóveda segura de Codewolf, manteniendo su comportamiento funcional dentro del runtime Go, la TUI, los subagentes y las políticas actuales de seguridad.

# Decisiones tomadas
- Crear una sola herramienta `ssh_remote` con las mismas acciones funcionales de Codewolf para servidores guardados, conexiones persistentes, vault, ejecución, shell y SFTP.
- Mantener perfiles sin secretos en `~/.li/ssh-servers.json` y credenciales cifradas en `~/.li/ssh-secrets.enc`.
- Cifrar la bóveda con scrypt (`N=32768`, `r=8`, `p=1`) y AES-256-GCM; la contraseña maestra nunca se almacena.
- Solicitar secretos mediante una entrada local enmascarada transportada por `interaction.Bridge`, fuera del modelo y de la persistencia de sesiones. El documento 118 reemplaza la pantalla completa original por un popup dentro del chat.
- Cerrar todas las conexiones y bloquear la bóveda mediante `sshremote.ShutdownAll()` cuando termina el proceso.
- Crear `gitzip` sobre un backend común local/remoto, con ZIP, TAR y TAR.GZ y reglas ignore anidadas compatibles.
- Elevar el mínimo a Go 1.25.12 para usar versiones actuales y corregidas de `x/crypto` y `pkg/sftp`.
- Elevar la versión de Lilith a `0.3.0` por tratarse de un bloque funcional nuevo.

# Arquitectura actual
- `internal/interaction`: cola local y cancelable de prompts secretos y confirmaciones.
- `internal/tui/secure_prompt.go`: mensajes del puente interactivo; el render enmascarado actual vive en `permission_widget.go` según el documento 118.
- `internal/sshremote`: store de perfiles, bóveda, manager de conexiones, SFTP y shells.
- `internal/gitzip`: escaneo ignore y creación local de archivos.
- `internal/tools/ssh_remote.go`: contrato de herramienta y acciones SSH/vault.
- `internal/tools/gitzip.go`: creación, subida, empaquetado y extracción remota.
- `cmd/li/main.go`: ciclo de vida que cierra bridge, conexiones y bóveda.

# Librerías usadas
- `golang.org/x/crypto v0.54.0` para SSH, agent y scrypt.
- `github.com/pkg/sftp v1.13.11` para SFTP.
- `github.com/Microsoft/go-winio v0.6.2` para agentes SSH por named pipe en Windows.
- Librería estándar para AES-GCM, ZIP, TAR y GZIP.

# Archivos importantes modificados
- `cmd/li/main.go`
- `internal/config/config.go`
- `internal/subagents/runtime.go`
- `internal/tools/registry.go`
- `internal/tui/app.go`
- `internal/tui/config_screen.go`
- `internal/tui/plan_mode.go`
- `internal/tui/secure_prompt.go`
- `internal/interaction/*`
- `internal/sshremote/*`
- `internal/gitzip/*`
- `internal/tools/ssh_remote.go`
- `internal/tools/gitzip.go`
- `go.mod`, `go.sum`
- `README.md`, `install.md`, `AGENTS.md`

# Problemas encontrados
- Los secretos no podían pedirse desde una tool sin pasar por el mensaje/modelo.
- La arquitectura no tenía un ciclo de vida global para conexiones SSH y material descifrado.
- GitZip remoto necesitaba reutilizar una conexión y conservar las mismas reglas ignore del empaquetado local.
- Un `.git` anidado podía ser recorrido aunque sus entradas terminaran ignoradas.
- Las versiones seguras actuales de SSH/SFTP requieren Go 1.25.12.

# Soluciones implementadas
- Puente de interacción local con prompts enmascarados, confirmación y cancelación.
- Bóveda cifrada, memoria efímera, tres intentos de desbloqueo, cambio de contraseña y borrado explícito de campos.
- Perfiles reutilizables, autenticación por vault/env/key/agent, huella opcional, keepalive y conexiones persistentes.
- Compatibilidad con sockets Unix y named pipes de OpenSSH en Windows, con fallback local no intrusivo.
- Shell remota incremental, ejecución con stdout/stderr/exit code y operaciones SFTP.
- GitZip local/remoto con manifiesto explícito, protección de `.env`, allowlist de argumentos y exclusión absoluta de cualquier `.git`.
- Ajustes de seguridad individuales en `/config`.
- Pruebas de vault, store, manager, matcher, archivos, bridge y contrato de herramientas.

# Pendientes
- Ejecutar la suite oficial completa con Go 1.25.12 y dependencias reales en el runner y en Windows.
- Probar conexión contra un servidor SSH real con contraseña, clave cifrada y SFTP.
- Probar el flujo interactivo en Termux ARM64.
- La ejecución real contra agentes SSH de Windows, Unix y servidores remotos debe verificarse en hosts controlados; la compilación y los contratos quedan cubiertos por pruebas.

# Próximos pasos
1. Ejecutar `test.cmd -Vet` con Go 1.25.12.
2. Probar `ssh_remote` contra un servidor controlado y verificar cierre al salir.
3. Probar GitZip local, subida, creación remota y extracción sin `.git`/`.env`.
4. Publicar `v0.3.0` cuando la validación oficial complete.
