# ADR 0010: El frontend vive en un repositorio aparte; solo el backend se dockeriza

Estado: aceptada. Fecha: 2026-09-26. Reemplaza la parte de la SPA embebida del [ADR 0008](0008-spa-embebida-y-polling.md).

## Contexto

El ADR 0008 embebía la SPA en el binario de Go para que `docker compose up` levantara todo sin CORS. El dueño del proyecto prefiere el frontend en su propio repositorio, como una aplicación de React normal, y decidir más adelante si la despliega. La SPA nunca llegó a embeberse: el Dockerfile no tenía etapa de Node.

## Decisión

- El frontend está en `voltia-web` (React, Vite y TypeScript). Este repositorio contiene solo el backend, la API y la documentación.
- Solo el backend se dockeriza. `docker compose up` levanta `voltia` y `postgres`.
- En desarrollo, Vite corre con su proxy de `/api` hacia el servidor Go, así que el navegador ve un solo origen y la cookie de sesión funciona sin CORS.
- El polling del progreso del análisis (ADR 0008) se mantiene igual.

## Alternativas descartadas

- Seguir en un monorepo con `go:embed`: mantenía el comando único, pero el dueño no quiere el frontend dentro del repositorio del backend.
- Contenedor propio para el frontend en este compose: añade un servicio y una configuración de proxy que hoy no se necesitan.

## Consecuencias

- Si el frontend se despliega aparte, el hosting debe reescribir `/api` hacia el backend para conservar el mismo origen, o el backend debe aceptar CORS con credenciales y cookie `SameSite=None; Secure`. Ninguna de las dos está hecha.
- La entrega son dos repositorios. El README del backend enlaza al del frontend.
- El contrato entre ambos es `api/openapi.yaml`.
