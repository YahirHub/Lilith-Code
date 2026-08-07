# Formularios, auth y estados

## Formularios

Comprueba:
- labels/ayuda;
- validación cliente y servidor;
- disabled/submitting;
- doble submit;
- errores visibles y recuperables;
- preservación razonable de datos después de error;
- navegación/cancelación.

No envíes formularios destructivos durante una auditoría salvo que el entorno sea explícitamente de prueba y el usuario lo pida.

## Secretos

Para contraseña/token usa `browser fill_secret`. El valor se introduce en el prompt local y no debe aparecer en el prompt del agente ni en logs.

## Estados

Cada página/asíncrono relevante debe manejar según aplique:
- loading inicial;
- loading de acción;
- empty;
- error;
- success;
- offline/no conexión;
- permiso insuficiente;
- sesión expirada.

Evita mostrar UI de un estado anterior mientras los datos de una nueva conversación/página todavía no terminaron de cargar.

## Auth/roles

Prueba rutas según el rol delegado. No concluyas que una ruta está rota si el servidor la bloquea correctamente por permisos. Registra status/redirect y estado visible.
