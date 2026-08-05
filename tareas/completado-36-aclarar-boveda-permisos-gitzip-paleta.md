# Estado
Completado

# Objetivo
Eliminar la confusión entre contraseña maestra y contraseña remota, ampliar las decisiones SSH, hacer GitZip selectivo por rutas y corregir ranking/autocompletado/color de comandos y skills.

# Implementación
- Tipos de secreto explícitos para bóveda, servidor remoto y clave privada.
- Reutilización garantizada de la bóveda abierta al guardar varias credenciales.
- Aprobaciones SSH para una vez, sesión, proyecto o denegación.
- Persistencia acotada por proyecto/acción/destino y control para borrar permisos recordados.
- `source_path`, `include_paths` y `exclude_paths` disponibles en GitZip local/remoto.
- Ranking exacto/prefijo/subcadena/subsecuencia para la paleta slash.
- `Tab` agrega un espacio después de comandos y skills.
- Skills diferenciadas con color secundario en paleta y editor.
- Pruebas de regresión y documentación técnica actualizadas.

# Resultado
Los popups muestran siempre el tipo correcto de contraseña y una bóveda abierta no vuelve a pedir la maestra durante la misma ejecución. Los permisos SSH pueden recordarse con alcance controlado, GitZip puede empaquetar subconjuntos explícitos y la paleta slash prioriza/autocompleta correctamente comandos y skills.
