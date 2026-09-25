# ADR 0007: Gemini 3.8 Flash como proveedor por defecto, Claude intercambiable

Estado: aceptada. Fecha: 2026-09-25.

## Contexto

El modelo solo redacta, así que un modelo rápido y con capa gratuita alcanza. El proveedor debe poder cambiarse sin tocar el resto.

## Decisión

- El puerto `ports.Explainer` lo implementan `llm.Explainer` (que envuelve a un `Generator`) y las plantillas.
- El proveedor por defecto es Gemini con el modelo `gemini-3.8-flash`, configurable con `LLM_MODEL`. Se llama por REST con `net/http` en lugar del SDK oficial: es un solo POST y evita una dependencia.
- El prompt, el esquema JSON, la guarda de números, la caché y el respaldo a plantillas viven en `llm.Explainer` y no dependen del proveedor. Añadir Claude es escribir otro `Generator`.
- Sin `GEMINI_API_KEY` el servidor arranca con plantillas y lo dice en el log.

## Alternativas descartadas

- SDK oficial de Gemini: más superficie de la que se usa.
- Solo plantillas: no muestra lo que aporta el modelo.

## Consecuencias

- En la capa gratuita de Gemini el contenido puede usarse para mejorar productos de Google. Es aceptable con este dataset de prueba; en producción se usaría la capa de pago con opt-out.
- Estado: el adaptador de Gemini está probado contra un servidor local, no contra la API real. El adaptador de Claude no está escrito, y `LLM_PROVIDER` solo acepta `gemini` o `template`.
