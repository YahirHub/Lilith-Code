# Lilith

Lilith (`li`) es un agente de programación que trabaja directamente desde la
terminal. Puede analizar un proyecto, editar archivos, ejecutar comandos,
delegar trabajo a otros agentes y conservar tus conversaciones para continuar
más tarde.

Su interfaz está pensada para usarse con teclado o mouse tanto en Windows como
en Linux, macOS, servidores por SSH y Termux.

## Funciones principales

- Conversación en terminal con Markdown, razonamiento y ejecución de herramientas.
- Proveedores personalizados, ChatGPT Codex y modelos gratuitos de OpenCode Free.
- Cambio de modelo sin reiniciar la aplicación.
- Historial persistente de conversaciones con búsqueda, reanudación y borrado.
- Agentes y subagentes para dividir tareas grandes o ejecutar trabajo en paralelo.
- Skills reutilizables que pueden activarse o desactivarse individualmente.
- Búsqueda web mediante distintos motores configurables.
- Modos Build, Plan y Goal para implementar, planificar o mantener un objetivo.
- Lectura, edición y validación de proyectos en distintos lenguajes.
- Recuperación de sesiones y continuidad al trabajar en VPS o conexiones inestables.

## Recorrido visual

<table>
  <tr>
    <td width="66%">
      <a href="docs/images/pantalla-principal.png">
        <img src="docs/images/pantalla-principal.png" alt="Pantalla principal de Lilith">
      </a>
    </td>
    <td valign="top">
      <strong>Pantalla principal</strong>
      <ul>
        <li>Muestra el proyecto en el que estás trabajando.</li>
        <li>Indica el proveedor y modelo que están activos.</li>
        <li>Permite escribir solicitudes, instrucciones o un objetivo persistente.</li>
        <li>Enseña de forma visible cuánto contexto se está utilizando.</li>
        <li>Concentra la conversación, el progreso y las acciones del agente.</li>
      </ul>
    </td>
  </tr>
  <tr>
    <td width="66%">
      <a href="docs/images/inicio-sesion.png">
        <img src="docs/images/inicio-sesion.png" alt="Pantalla para conectar un proveedor">
      </a>
    </td>
    <td valign="top">
      <strong>Conexión de proveedor</strong>
      <ul>
        <li>Permite conectar un endpoint compatible mediante una API key.</li>
        <li>Incluye acceso con una cuenta de ChatGPT Codex.</li>
        <li>Ofrece una ruta gratuita sin necesidad de configurar credenciales.</li>
        <li>Puede manejarse completamente con teclado o mediante clic.</li>
        <li>La pantalla puede abrirse nuevamente con <code>/login</code>.</li>
      </ul>
    </td>
  </tr>
  <tr>
    <td width="66%">
      <a href="docs/images/selector-modelos.png">
        <img src="docs/images/selector-modelos.png" alt="Selector de modelos de Lilith">
      </a>
    </td>
    <td valign="top">
      <strong>Selector de modelos</strong>
      <ul>
        <li>Agrupa los modelos disponibles por proveedor.</li>
        <li>Muestra la capacidad de contexto de cada opción.</li>
        <li>Permite filtrar rápidamente escribiendo en el buscador.</li>
        <li>Marca claramente cuál es el modelo activo.</li>
        <li>Puede actualizar los catálogos sin cerrar la conversación.</li>
      </ul>
    </td>
  </tr>
  <tr>
    <td width="66%">
      <a href="docs/images/historial-conversaciones.png">
        <img src="docs/images/historial-conversaciones.png" alt="Historial de conversaciones de Lilith">
      </a>
    </td>
    <td valign="top">
      <strong>Historial de conversaciones</strong>
      <ul>
        <li>Lista las conversaciones guardadas del proyecto actual.</li>
        <li>Muestra antigüedad y cantidad de turnos de cada sesión.</li>
        <li>Permite buscar por el contenido o título de la conversación.</li>
        <li>Una sesión puede reanudarse exactamente donde quedó.</li>
        <li>Las conversaciones que ya no se necesiten pueden eliminarse.</li>
      </ul>
    </td>
  </tr>
  <tr>
    <td width="66%">
      <a href="docs/images/configuracion-general.png">
        <img src="docs/images/configuracion-general.png" alt="Configuración general de Lilith">
      </a>
    </td>
    <td valign="top">
      <strong>Configuración general</strong>
      <ul>
        <li>Activa o desactiva las instrucciones persistentes de Lilith.</li>
        <li>Ofrece compatibilidad con archivos y reglas de Claude.</li>
        <li>Controla la memoria automática utilizada entre sesiones.</li>
        <li>Permite habilitar o deshabilitar los hooks compatibles.</li>
        <li>Reúne las preferencias principales en una sola pantalla.</li>
      </ul>
    </td>
  </tr>
  <tr>
    <td width="66%">
      <a href="docs/images/configuracion-skills.png">
        <img src="docs/images/configuracion-skills.png" alt="Configuración de skills de Lilith">
      </a>
    </td>
    <td valign="top">
      <strong>Skills</strong>
      <ul>
        <li>Incluye un interruptor general para todas las habilidades.</li>
        <li>Cada skill puede activarse o desactivarse de forma independiente.</li>
        <li>Muestra una explicación clara de la finalidad de cada habilidad.</li>
        <li>Indica si la skill proviene del usuario, del proyecto o de Lilith.</li>
        <li>Las preferencias se conservan para las siguientes sesiones.</li>
      </ul>
    </td>
  </tr>
  <tr>
    <td width="66%">
      <a href="docs/images/configuracion-busqueda.png">
        <img src="docs/images/configuracion-busqueda.png" alt="Configuración de motores de búsqueda">
      </a>
    </td>
    <td valign="top">
      <strong>Motores de búsqueda</strong>
      <ul>
        <li>Reúne los proveedores de búsqueda compatibles en un solo lugar.</li>
        <li>Muestra cuáles están configurados y cuáles se encuentran activos.</li>
        <li>Permite abrir cada motor para agregar o actualizar sus credenciales.</li>
        <li>Admite Tavily, Brave Search, Exa, Linkup, Firecrawl, SerpApi y Zenserp.</li>
        <li>Puede utilizarse con teclado o mouse.</li>
      </ul>
    </td>
  </tr>
</table>

## Agentes y orquestación

Lilith puede delegar partes de una tarea a agentes especializados. Estos agentes
pueden trabajar en primer plano, continuar en segundo plano o dividir el trabajo
en nuevos subagentes cuando la tarea lo requiere.

El usuario puede seguir conversando, cancelar el trabajo, cambiar de sesión y
retomar tareas guardadas sin mezclar resultados entre conversaciones distintas.

## Skills configurables

Las skills agregan metodologías o capacidades reutilizables. Lilith incluye la
skill `ponytail-development` y también reconoce skills del usuario y del
proyecto.

Puedes administrarlas en:

```text
/config > Skills
```

También puedes invocar una directamente:

```text
/skill:ponytail-development revisar este proyecto
```

## Comandos útiles

| Comando | Acción |
|---|---|
| `/login` | Conectar o cambiar el proveedor. |
| `/models` | Elegir otro modelo. |
| `/history` | Abrir el historial de conversaciones. |
| `/config` | Administrar preferencias, skills, búsqueda y seguridad. |
| `/goal` | Crear o revisar el objetivo persistente del proyecto. |
| `/clear` | Iniciar una conversación limpia. |
| `/exit` | Cerrar Lilith. |

## Atajos principales

| Atajo | Acción |
|---|---|
| `Enter` | Enviar el mensaje. |
| `Alt+Enter` | Encolar un mensaje para después del trabajo actual. |
| `Shift+Enter` o `Ctrl+Enter` | Insertar una nueva línea. |
| `Tab` | Cambiar entre Build, Plan y Goal. |
| `Esc` | Cancelar la tarea activa o volver a la pantalla anterior. |
| `Ctrl+C` | Limpiar el texto escrito en el campo de entrada. |

## Instalación

Las instrucciones para Windows, Linux, Termux, actualización, compilación,
pruebas y publicación se encuentran en [`install.md`](./install.md).

## Estado de las pruebas

La suite completa se valida con:

```cmd
test.cmd
```

La ejecución más reciente en Windows completó correctamente todos los paquetes,
incluidos `internal/subagents` e `internal/tui`, y finalizó con:

```text
all modules verified
Validación completada correctamente.
```
