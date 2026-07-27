# 015 — Corrección OAuth Codex y URL manual

## Contexto

El login OAuth de ChatGPT/Codex fallaba con `Invalid authorization request` y el flujo abría automáticamente una URL en el navegador, lo cual no es deseado para una CLI/TUI.

## Investigación

Se contrastó el flujo con implementaciones públicas compatibles con Codex y con el código oficial de Codex CLI. Hallazgos relevantes:

- El cliente OAuth público es `app_EMoamEEZ73f0CkXaXp7hrann`.
- El issuer correcto es `https://auth.openai.com`.
- El flujo principal debe ser Authorization Code + PKCE.
- Los puertos de callback permitidos por OpenAI para Codex son `1455` y fallback `1457`.
- El scope oficial actual incluye `api.connectors.read` y `api.connectors.invoke` además de `openid profile email offline_access`.
- La CLI no debe abrir el navegador automáticamente; debe mostrar la URL y dejar la acción en manos del usuario.

## Cambios

- Actualizado el scope OAuth de Codex a:
  `openid profile email offline_access api.connectors.read api.connectors.invoke`.
- Añadido fallback de callback local en el puerto `1457` si `1455` está ocupado.
- El redirect URI ahora se construye según el puerto realmente abierto.
- Eliminada la apertura automática del navegador desde la TUI.
- La pantalla de login ahora muestra la URL OAuth y ofrece un botón/atajo `C Copiar URL`.
- El flujo de dispositivo mantiene el código visible y permite copiar código (`C`) o URL (`U`).
- El intervalo del device flow ahora acepta valores numéricos o strings devueltos por el servidor.

## Pruebas mínimas

- Compilación y tests selectivos de proveedor/TUI con Go.
- No se ejecutó un login real completo porque requiere credenciales interactivas del usuario en OpenAI.

## Commit sugerido

**Summary:** Corregir OAuth Codex y mostrar URL manual con copia en TUI

**Description:**
- Corrige el scope OAuth de Codex según el flujo oficial actual, incluyendo permisos de conectores.
- Usa callback local en `1455` con fallback oficial a `1457`.
- Elimina la apertura automática del navegador y muestra la URL en la TUI.
- Añade acciones para copiar la URL/código desde la terminal mediante OSC52.
- Hace más tolerante el parseo del intervalo del device flow.