# Estado
Completado

# Objetivo
Abrir la bóveda SSH sólo cuando una conexión necesite credenciales guardadas, integrar secretos y aprobaciones en el chat, aclarar cada tipo de contraseña y recuperar automáticamente las conexiones ante `EOF` sin cambiar su identificador.

# Implementación
- Eliminado el desbloqueo de la bóveda durante el arranque.
- Desbloqueo perezoso mediante popup enmascarado dentro del chat.
- Textos separados para contraseña maestra de la bóveda, contraseña del servidor y passphrase de clave privada.
- Bóveda reutilizada durante todo el proceso después del primer desbloqueo.
- Conexiones lógicas estables con generación de transporte, contador de reconexiones, monitor de cierre y reparación automática.
- Credenciales solicitadas por popup reutilizadas sólo en memoria para que la reconexión no vuelva a pedirlas.
- Comandos SSH sin timeout artificial predeterminado, adecuados para builds y despliegues largos.
- Normalización de `EOF` y ausencia de exit status sin exigir `close`/`connect_server`.
- Reintentos de apertura de canal, PTY, shell y operaciones SFTP.
- Políticas SSH `critical_only`, `every_action`, `commands_only`, `trust_model` y `custom`.
- Pantalla `/config > Seguridad > SSH Remoto` con siete categorías configurables.
- Widget de aprobación dentro del chat, ampliado posteriormente con alcances de sesión y proyecto.
- GitZip sin confirmación genérica; la protección independiente de `.env` se conserva.
- Migración automática del antiguo `sshSafeMode`.

# Resultado
El CLI no solicita la contraseña maestra al arrancar. Una conexión que usa la bóveda la solicita sólo al necesitarla y después la reutiliza. Los cortes del transporte ya no obligan al modelo a crear conexiones nuevas: el mismo `connection_id` se repara automáticamente y comunica de forma segura cuando un comando quedó con estado de salida incierto.
