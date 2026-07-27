# 016 · Transporte Codex sobre la Responses API

## Problema

Tras el login OAuth, cualquier petición al proveedor `openai-codex`
respondía con una página HTML (404/403). El motivo: la suscripción ChatGPT
Plus/Pro no expone `/v1/chat/completions`, sino la Responses API en
`https://chatgpt.com/backend-api/codex/responses`, con cabeceras específicas.

## Cambios

- `internal/secrets/secrets.go`: nuevo campo `AccountID` en `OAuthTokens`
  (compatible con el JSON existente).
- `internal/providers/openai/chatgpt_oauth.go`: `postToken` decodifica el
  `id_token` JWT y guarda `chatgpt_account_id` (o el primer `organizations[].id`
  como fallback). Se usa como cabecera `chatgpt-account-id`.
- `internal/providers/openai/codex_transport.go` (nuevo): traduce
  `Messages`/`Tools` estilo chat-completions al esquema Responses (`input[]`,
  `instructions`, `function_call`/`function_call_output`, `tools` planos con
  `strict:false`) y parsea los eventos SSE `response.output_text.delta`,
  `response.function_call_arguments.delta`, `response.output_item.*`,
  `response.completed` y `response.failed`.
- `internal/providers/openai/client.go`: `do()` despacha a `streamCodex` cuando
  el proveedor es `openai-codex`.

## Headers enviados

```
Authorization: Bearer <access_token>
OpenAI-Beta: responses=experimental
originator: codex_cli_rs
session_id: <uuid>
chatgpt-account-id: <extraído del id_token>
Accept: text/event-stream
```

## Importante para el usuario

Las sesiones creadas antes de este cambio **no tienen `AccountID`** guardado.
Basta con volver a hacer `/login` una vez para que el flujo lo persista.

## Commit

**Summary:** Rutar Codex por la Responses API con headers oficiales

**Description:** El proveedor `openai-codex` ahora habla con
`https://chatgpt.com/backend-api/codex/responses` en lugar de
`/chat/completions`. Se traducen mensajes y herramientas al esquema Responses,
se parsean sus eventos SSE (`response.output_text.delta`,
`response.function_call_arguments.delta`, `response.output_item.*`,
`response.completed`, `response.failed`) y se envían los headers exigidos por
el backend: `OpenAI-Beta: responses=experimental`, `originator: codex_cli_rs`,
`session_id` y `chatgpt-account-id` extraído del `id_token` (guardado en
`OAuthTokens.AccountID`). Los usuarios ya autenticados deben ejecutar `/login`
una vez para persistir el nuevo campo.
