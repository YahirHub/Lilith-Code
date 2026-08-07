# Responsive y accesibilidad

## Responsive

Revisa al menos las anchuras/entornos que el proyecto soporte. Si el controlador del navegador no permite cambiar viewport directamente, usa las herramientas de test existentes o reporta esa limitación; no inventes una validación móvil que no realizaste.

Busca:
- overflow horizontal;
- botones cortados;
- barras fijas que tapan contenido;
- modales fuera de viewport;
- tablas sin estrategia móvil;
- touch targets demasiado pequeños;
- texto truncado sin acceso al contenido.

## Accesibilidad práctica

Comprueba:
- controles interactivos accesibles en snapshot;
- labels/nombres de botones;
- foco visible y orden lógico cuando puedas probar teclado;
- Escape/cierre donde corresponda;
- contraste suficiente según el sistema de diseño;
- estados no comunicados sólo por color;
- elementos deshabilitados comprensibles.

## Teclado

Usa `key` para probar recorridos críticos sin mouse cuando sea viable. No conviertas una auditoría funcional en una certificación WCAG completa salvo que el usuario la solicite y haya herramientas suficientes.
