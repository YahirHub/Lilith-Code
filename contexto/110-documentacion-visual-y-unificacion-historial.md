# 110 · Documentación visual y unificación del historial local

# Fecha

2026-08-04

# Objetivo

Actualizar la documentación pública con capturas reales de las pantallas principales de Lilith y consolidar en un único commit coherente los cambios locales posteriores a `874cd409746b9bac40496495d5d8179cd03c8569`, todavía no publicados.

# Decisiones tomadas

- Mantener intacto el commit `874cd40` que introdujo la skill Ponytail configurable.
- Fusionar los tres commits posteriores de auditoría del orquestador, corrección de módulos/tests y estabilización de cancelación anidada junto con la actualización documental actual.
- Dejar `README.md` orientado a usuarios: funciones, recorrido visual, comandos y atajos.
- Trasladar la explicación de instalación, pruebas, compilación, shells, orquestación, skills, escritura segura, inteligencia de código y releases a `install.md`.
- Guardar las imágenes dentro de `docs/images/` con nombres estables y descriptivos.
- Usar una tabla HTML para relacionar cada captura con una descripción funcional sin detalles internos de implementación.

# Arquitectura actual

```text
README.md
  ├─ presentación del producto
  ├─ funciones principales
  ├─ tabla HTML de capturas
  ├─ agentes y skills
  ├─ comandos y atajos
  └─ enlace a instalación

install.md
  ├─ instalación por sistema
  ├─ compilación
  ├─ pruebas
  └─ referencia técnica

docs/images/
  ├─ pantalla-principal.png
  ├─ inicio-sesion.png
  ├─ selector-modelos.png
  ├─ historial-conversaciones.png
  ├─ configuracion-general.png
  ├─ configuracion-skills.png
  └─ configuracion-busqueda.png
```

# Librerías usadas

No se añadieron dependencias.

# Archivos importantes modificados

- `README.md`
- `install.md`
- `AGENTS.md`
- `contexto/000-contexto-maestro.md`
- `contexto/110-documentacion-visual-y-unificacion-historial.md`
- `tareas/completado-27-documentacion-visual-y-unificar-historial.md`
- `docs/images/*.png`

# Problemas encontrados

- El README concentraba explicaciones internas extensas que dificultaban entender rápidamente qué ofrece Lilith.
- No existía una galería visual para mostrar la interfaz real.
- Los cambios posteriores a la skill Ponytail estaban repartidos en tres commits de estabilización estrechamente relacionados y aún no se habían publicado.

# Soluciones implementadas

- Se creó un recorrido visual con las siete capturas proporcionadas y descripciones centradas en lo que puede hacer el usuario.
- Se mantuvieron fuera de las tablas conceptos internos como paquetes, concurrencia, checksums, tags de compilación o persistencia atómica.
- La referencia técnica se centralizó en `install.md` para conservar toda la información útil sin recargar la portada pública.
- Se preparó el historial para reemplazar los tres commits locales posteriores a `874cd40` por un solo commit nuevo.

# Pendientes

- Revisar cómo se renderiza la tabla HTML en GitHub después del push.
- Publicar el historial reescrito únicamente mediante push normal si la rama remota todavía termina en `874cd40` o antes. Si esos commits intermedios aparecieran en remoto por otra vía, usar `--force-with-lease` y no `--force`.

# Próximos pasos

1. Importar el ZIP completo o reemplazar la copia local.
2. Ejecutar `git log --oneline` para confirmar la consolidación.
3. Ejecutar `test.cmd` si se desea repetir la suite antes del push.
4. Hacer push de `main`.
