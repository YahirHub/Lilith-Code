---
name: frontend-development
description: Use for implementing, fixing or reviewing web frontends and UI: inspect existing pages/routes/components, preserve the design system, verify changes in a real browser, inspect console/network errors, test navigation/forms/states, and delegate exhaustive page audits to the isolated frontend-browser-auditor agent.
user-invocable: true
model: inherit
argument-hint: "[cambio o auditoría frontend]"
when_to_use: |
  Any task that changes or validates HTML/CSS/JS, templates, SPA/SSR pages, dashboards, responsive UI, forms, navigation, browser behavior, frontend errors or visual regressions.
---

# Desarrollo frontend — índice y regla de verificación

Esta skill es modular. **No cargues todos los archivos.** Selecciona sólo los módulos necesarios con `skill_read`.

## Regla obligatoria

Una tarea frontend no termina al compilar. Cuando exista una aplicación ejecutable/revisable:

1. **Antes de editar**, inspecciona las páginas/rutas/componentes relevantes y el diseño existente; no diseñes a ciegas.
2. **Antes de corregir un error**, reproduce cuando sea viable e inspecciona consola y red del navegador.
3. **Después de editar**, vuelve a abrir la UI real, prueba las rutas/estados afectados y revisa consola/network.
4. Para una revisión amplia de varias páginas, **delega al subagente `frontend-browser-auditor`** en vez de llenar el contexto principal con snapshots, HTML, logs y requests. Dale URL/base URL, alcance, credenciales sólo mediante mecanismos seguros y qué flujos no debe modificar.
5. El padre debe recibir del auditor sólo hallazgos accionables y usar el detalle completo únicamente si necesita corregir un fallo concreto.

## Enrutador

| Necesidad | Recurso |
|---|---|
| flujo antes/durante/después de implementar | `references/workflow.md` |
| auditoría exhaustiva con navegador/subagente | `references/browser-audit.md` |
| consola JS, network, APIs, carga y errores | `references/console-network.md` |
| formularios, auth, estados loading/error/empty | `references/forms-states.md` |
| responsive, accesibilidad, teclado y regresiones visuales | `references/responsive-accessibility.md` |
| checklist final y formato de reporte | `references/verification-report.md` |

## Límites

- No cambies backend sólo para ocultar un error frontend sin demostrar la causa.
- No limpies errores de consola suprimiéndolos; corrige la causa o documenta por qué son externos/esperados.
- No uses datos destructivos en formularios de producción durante una auditoría.
- Usa `fill_secret` para credenciales sensibles del navegador; nunca pongas contraseñas/tokens en argumentos visibles.
- Después de `navigate`/`reload`, vuelve a obtener `scripts` antes de `search_source` porque los IDs CDP son por documento.
