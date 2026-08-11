# 141. Perfiles existentes de navegador y cookies JSON

## Objetivo

Permitir que Lilith reutilice autenticación ya disponible sin obligar al agente a
pedir usuario/contraseña cada vez. Se cubren dos flujos complementarios:

1. descubrir y seleccionar perfiles Chromium existentes cuando el navegador
   expone una sesión CDP reutilizable;
2. importar cookies desde un JSON exportado hacia cualquier sesión administrada
   por Lilith, especialmente un perfil `persistent` aislado.

## Perfiles existentes

Se añadió `ProfileExisting` y la acción `browser action=profiles`.

El descubrimiento reconoce los roots habituales de Chrome, Chromium, Edge, Brave,
Vivaldi y Opera en Windows, macOS y Linux. Para navegadores Chromium con `Local
State`, Lilith usa `profile.info_cache` para obtener únicamente el nombre visible y
el subdirectorio (`Default`, `Profile 1`, etc.) y marca el último perfil usado.
No se extraen `user_name`, cookies, contraseñas ni otros datos de cuenta.

Cada perfil recibe un `profile_id` estable derivado de la ruta y el subdirectorio.
La ruta raíz no se serializa en `action=profiles`, no se devuelve en el estado de
una sesión `existing` y tampoco se persiste en `browser.json` cuando la selección
se hizo mediante `profile_id`; se resuelve localmente cuando comienza la sesión.
El `DevToolsActivePort`, cuando existe, se analiza localmente y se comprueba que su
puerto loopback siga escuchando antes de marcar un perfil como adjuntable; el
WebSocket no se devuelve en la lista de perfiles. No se hace un handshake WebSocket
durante `profiles`, porque Chrome 144+ puede pedir autorización al usuario. El
handshake CDP ocurre únicamente durante un `start` explícito y usa directamente la
ruta WebSocket, sin depender de `/json/version`. Como ese endpoint pertenece al `User Data` completo y no a un subdirectorio
arbitrario, cuando hay varios perfiles Lilith sólo marca adjuntable
el último perfil usado y no finge poder seleccionar por CDP otro perfil hermano.

`start` y la configuración predeterminada aceptan:

- `profile_mode=existing`;
- `profile_id`;
- `profile_directory`;
- `user_data_dir`.

Si se usa `profile_id`, éste resuelve internamente el root y el subdirectorio. El
modo no intenta modificar, copiar ni relanzar a la fuerza el perfil personal. Si
el root personal no expone CDP, se devuelve un error orientando al flujo de Remote
Debugging compatible o a la importación de cookies sobre un perfil aislado.

Esta restricción es intencional: las versiones modernas de Chrome endurecieron el
remote debugging sobre el directorio de datos predeterminado, y un navegador ya
abierto también puede mantener bloqueos sobre su perfil. Lilith no debe sortear
esas protecciones manipulando el perfil real.

## Importación JSON de cookies

Se añadió `internal/browser/cookies.go` y `browser action=import_cookies`.
`cookie_path` también puede acompañar a `action=start`; en ese caso el runtime:

1. inicia la sesión en `about:blank`;
2. lee y normaliza el JSON localmente;
3. aplica las cookies con CDP `Network.setCookies`;
4. navega a `url` sólo después de completar la importación.

Formatos admitidos:

- array JSON de cookies, como los exportes habituales de extensiones;
- objeto con propiedad `cookies` o `Cookies`.

Campos normalizados cuando existen:

- `name` / `value`;
- `domain` o `url`;
- `hostOnly` para conservar cookies ligadas exactamente al host;
- `path`;
- `secure`;
- `httpOnly`;
- `sameSite` (`strict`, `lax`, `none`, `no_restriction`);
- `expirationDate`, `expires`, `expiry` y variantes comunes.

Cuando una cookie carece de dominio/URL puede usarse la `url` de la acción como
contexto. `SameSite=None` sin `Secure`, cookies particionadas y entradas con
semántica no conservable se omiten en vez de degradarse silenciosamente. También
se validan las reglas de los prefijos `__Secure-` y `__Host-` antes de enviar la
cookie al navegador.

## Límites y secretos

- archivo máximo: 16 MiB;
- máximo: 10.000 cookies;
- sólo archivos regulares;
- `~`, rutas absolutas y rutas relativas al proyecto son aceptadas por el tool;
- los valores nunca se serializan en la respuesta;
- tampoco se devuelven dominios ni el contenido del JSON;
- la respuesta pública se limita a `imported` y `skipped`;
- errores de parseo no incorporan el contenido de las cookies.

El modelo debe trabajar sólo con `cookie_path`; no debe usar `read_file`, shell ni
otra herramienta para inspeccionar el JSON antes de importarlo.

## Flujo recomendado

Para un login que debe sobrevivir reinicios de Lilith:

1. exportar las cookies del sitio a un JSON;
2. iniciar `profile_mode=persistent` con un nombre dedicado;
3. pasar `cookie_path` y la URL destino en el primer `start`;
4. en ejecuciones posteriores reutilizar el perfil persistente, sin reimportar si
   la sesión sigue vigente.

El perfil personal se reserva para el caso explícito en que el usuario quiera que
Lilith se adjunte a una instancia CDP existente.

## Archivos principales

- `internal/browser/types.go`
- `internal/browser/profiles.go`
- `internal/browser/cookies.go`
- `internal/browser/manager.go`
- `internal/browser/manager_test.go`
- `internal/browser/profiles_test.go`
- `internal/browser/cookies_test.go`
- `internal/tools/browser.go`
- `internal/tools/browser_test.go`
- `README.md`
- `AGENTS.md`
- `contexto/000-contexto-maestro.md`

## Validación requerida

Antes de publicar el cambio, validar en el toolchain oficial del proyecto:

```bash
gofmt -w internal/browser/*.go internal/tools/browser.go
git diff --check
go test -tags=grammar_set_core ./internal/browser ./internal/tools
go test -tags=grammar_set_core ./...
go test -tags=grammar_set_core -race ./...
go vet -tags=grammar_set_core ./...
CGO_ENABLED=0 go build -tags=grammar_set_core ./cmd/li
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags=grammar_set_core ./cmd/li
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -tags=grammar_set_core ./cmd/li
```
