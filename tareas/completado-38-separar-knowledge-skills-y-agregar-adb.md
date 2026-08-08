# Separar Knowledge de Skills y agregar ADB

## Objetivo

Rehacer el commit de `/init`, Knowledge y Goal → Build sin duplicar las guías
operativas de Docker y Git que ya pertenecen a Agent Skills. Incorporar una
referencia Knowledge de Android Debug Bridge para conexiones USB y Wi-Fi.

## Alcance

- Mantener Docker/Compose y Git/GitHub exclusivamente en sus Skills.
- Reservar Knowledge para referencias técnicas consultables que no activan un
  workflow.
- Añadir ADB por USB, depuración inalámbrica con pairing y el flujo TCP/IP
  heredado, incluyendo diagnóstico y medidas de seguridad.
- Actualizar pruebas, documentación y contexto persistente.
- Validar Linux, Windows y compilación Android antes de rehacer el commit.

## Estado

Completado y verificado con pruebas normales y race, `go vet`, build estático
Linux, build Windows amd64 y compilación de tests Android arm64.
