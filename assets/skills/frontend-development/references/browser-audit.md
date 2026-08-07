# Auditoría con navegador aislado

## Cuándo delegar

Usa `Agent` con `subagent_type="frontend-browser-auditor"` cuando:
- cambias layout/navegación compartida;
- hay 3+ páginas/rutas que revisar;
- quieres inspeccionar toda la app;
- el bug involucra consola/red entre páginas;
- una auditoría completa produciría demasiados snapshots/logs para el contexto principal.

No uses `fork` para este caso: el auditor debe recibir una tarea compacta y trabajar en contexto aislado.

## Prompt mínimo al auditor

Incluye:
- base URL o cómo identificarla;
- rutas/flujos prioritarios;
- estado esperado del servidor;
- si puede usar login y qué cuenta/rol (secreto siempre mediante `fill_secret`);
- acciones prohibidas (eliminar datos, pagos, envíos reales, cambios administrativos);
- qué cambio acaba de implementarse y qué regresiones vigilar.

Ejemplo conceptual:

```text
Audita http://localhost:8080 después del cambio de navegación. Inventaría las rutas reales desde el proyecto y revisa todas las páginas alcanzables del rol admin. No modifiques archivos ni datos persistentes. Inspecciona console/network por página y devuelve sólo fallos accionables, ruta, pasos y evidencia mínima.
```

## Qué debe hacer el auditor

1. Inventariar rutas desde código y navegación, sin adivinar URLs.
2. Iniciar/reutilizar una sesión de navegador aislada.
3. Limpiar/capturar consola y red por ruta cuando sea posible.
4. Navegar cada página y esperar estabilidad razonable.
5. Obtener snapshot suficiente para confirmar contenido/controles.
6. Probar interacciones no destructivas importantes.
7. Registrar HTTP fallidos, JS errors, pantallas en blanco, loading infinito, enlaces rotos y estados incoherentes.
8. Cerrar la sesión si fue creada sólo para la auditoría.
9. Devolver un resumen compacto al padre; no volcar HTML completo ni cientos de requests.

## Revisión después de corregir

Si el padre corrige un bug hallado, puede reanudar el mismo task_id del auditor para verificar sólo las rutas afectadas, evitando repetir la auditoría completa.
