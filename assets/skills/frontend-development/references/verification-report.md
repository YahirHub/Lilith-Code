# Verificación y reporte frontend

## PASS mínimo por ruta

Una ruta puede marcarse PASS cuando:
- navega/carga;
- el contenido esperado aparece;
- no queda loading infinito;
- no hay error JS crítico nuevo;
- requests esenciales terminan correctamente o con estados esperados;
- interacción principal no destructiva funciona si aplica.

## Hallazgo

Por cada fallo devuelve sólo:
- ruta;
- severidad;
- pasos mínimos;
- esperado;
- real;
- console/network relevante;
- componente/archivo probable sólo si está respaldado por evidencia.

## Resumen del auditor al padre

Preferir una tabla compacta:

```text
Rutas: 14 revisadas / 12 PASS / 2 FAIL
Críticos: 0
Altos: 1
Medios: 1

FAIL /usuarios: GET /api/users -> 500; tabla queda en loading.
FAIL /perfil: botón Guardar sin nombre accesible; funcionalmente guarda.
```

No copies HTML completo, todas las requests, snapshots gigantes ni logs sanos. El objetivo del subagente es aislar ese ruido.

## Re-test

Después de una corrección, revisa primero la ruta fallida y una ruta vecina que comparta el componente. Si el cambio afecta layout global, repite la matriz completa o el conjunto representativo definido por el proyecto.
