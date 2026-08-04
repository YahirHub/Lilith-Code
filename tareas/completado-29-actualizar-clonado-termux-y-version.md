# Objetivo

Corregir la instalación de Termux para que no resuelva ni fije tags o commits antiguos, clone únicamente la punta actual de la rama predeterminada con historial superficial, compile desde esa fuente y eleve la versión de Lilith para publicar un release nuevo.

# Estado

Completado y validado mediante simulación sin red.

# Validación prevista

- `sh -n install.sh`
- `python3 .github/scripts/test_install_sh.py`
- pruebas de `cmd/build` y versión
- `git diff --check`
