# Estado
Completado

# Objetivo
Adaptar a Lilith las capacidades de Codewolf para GitZip, SSH persistente y bóveda cifrada de credenciales, conservando la seguridad y el ciclo de vida de la arquitectura Go/TUI.

# Alcance
- Herramienta `ssh_remote` con perfiles, conexiones persistentes, SFTP, comandos, shell y archivos.
- Bóveda AES-256-GCM + scrypt, desbloqueada sólo durante el proceso y cerrada al salir.
- Herramienta `gitzip` local/remota respetando archivos ignore y protección de `.env`.
- Entrada secreta local enmascarada fuera del historial/modelo.
- Ajustes de seguridad, documentación y pruebas.

# Verificación
- Backend GitZip e interacción: tests, race y vet con Go local.
- Backend SSH: tests, race, vet y compilación Windows mediante harness API-compatible.
- Contratos de tools SSH/GitZip: tests, race y vet mediante harness de integración.
- Pendiente la suite oficial Go 1.25 y una conexión SSH real.
