# Fecha
2026-07-27

# Objetivo
Registrar preferencia del usuario sobre la entrega de código.

# Decisiones tomadas
- Tras **cualquier** edición de código, se debe generar un ZIP completo del
  proyecto y ofrecerlo para descarga.
- El ZIP vive en `/mnt/documents/lovable-go/lovable-go.zip` y se regenera con:
  ```bash
  cd /mnt/documents/lovable-go && rm -f lovable-go.zip \
    && cd contenido && zip -qr ../lovable-go.zip . -x "*.git*"
  ```

# Pendientes
Ninguno.

# Próximos pasos
Aplicar la preferencia en cada iteración futura sin necesidad de recordatorio.
