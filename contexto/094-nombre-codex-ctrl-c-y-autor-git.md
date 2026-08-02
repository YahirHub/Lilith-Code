# 094 — Nombre Codex, Ctrl+C y autor Git

Fecha: 2026-08-02

## Objetivo

Acortar el nombre visible del proveedor OAuth, convertir `Ctrl+C` en un atajo
seguro para limpiar el editor y corregir la identidad del autor en todo el
historial Git del proyecto.

## Nombre visible del proveedor

El identificador interno continúa siendo `openai-codex` y el transporte sigue
requiriendo una suscripción ChatGPT Plus/Pro. Únicamente cambia la etiqueta
mostrada al usuario:

```text
ChatGPT Codex
```

La etiqueta corta se usa en el proveedor bundled, OAuth, onboarding, pantalla de
login, selectores y mensajes que muestran `Provider.Name`. “Plus/Pro” queda como
descripción del requisito de cuenta y ya no forma parte del nombre del proveedor.

## Ctrl+C en el input

Tview interpreta el evento original de `Ctrl+C` como una orden de salida, por lo
que el runtime continúa clonándolo y entregándolo a Lilith. El chat ahora usa ese
evento para:

1. volver al área de interacción si el transcript estaba desplazado;
2. invalidar cualquier decisión pendiente del fallback de pegado;
3. vaciar completamente el textarea;
4. cerrar la paleta de comandos y recalcular la altura del input.

El atajo no cancela el turno activo, no modifica mensajes steering/follow-up ya
en cola y no cierra la aplicación. `Esc` sigue cancelando la tarea y `/exit`
sigue siendo la salida explícita. `Ctrl+Z` permanece neutralizado para evitar
suspender la TUI y dejar la terminal inconsistente.

## Autor Git

La identidad correcta del usuario es:

```text
YahirHub <217099863+YahirHub@users.noreply.github.com>
```

Se reescriben todos los commits cuyo autor o committer era `ThowiLabs`,
conservando fechas, mensajes, árboles y el correo. Los checkpoints internos
`Lilith Rewind <rewind@localhost>` no se cambian porque representan snapshots
técnicos generados por el sistema, no commits atribuidos al usuario.

Al reescribir historial cambian los hashes desde el commit afectado más antiguo.
Para actualizar GitHub será necesario un push protegido con:

```bash
git push --force-with-lease origin main
```

## Pruebas de regresión

- `Ctrl+C` limpia el editor y cierra la paleta.
- `Ctrl+C` conserva el turno y la cola.
- `Ctrl+Z` no suspende, cancela ni modifica el editor.
- El proveedor bundled usa exactamente `ChatGPT Codex`.
- El historial no contiene autor o committer `ThowiLabs`.

## Pruebas manuales recomendadas

1. Escribir varias líneas, pulsar `Ctrl+C` y comprobar que el input queda vacío.
2. Repetir durante streaming y comprobar que la respuesta continúa.
3. Encolar steering/follow-up, pulsar `Ctrl+C` sobre otro borrador y confirmar que
   la cola permanece.
4. Abrir `/login`, `/providers` y `/models`; la etiqueta debe ser
   `ChatGPT Codex`.
5. Ejecutar `git log --format='%an <%ae> | %cn <%ce>' --all` y confirmar que no
   aparece `ThowiLabs`.
