# ADR 0008: SPA de React embebida en el binario; polling para el progreso

Estado: aceptada. Fecha: 2026-09-25.

## Contexto

La ejecución debe ser un solo comando. El cálculo del análisis tarda unos 0,7 s, y la interfaz necesita mostrar sus siete etapas.

## Decisión

- La SPA de React con Vite se compila y se embebe en el binario con `go:embed`. Todo lo que no es `/api` la sirve, en el mismo origen, sin CORS.
- El progreso se consulta por polling (cada 800 ms aproximadamente) a `GET /api/v1/ai/analysis/{id}`. Cada etapa hace una pausa de 300 ms (`ANALYSIS_STAGE_DELAY`) para que el avance se pueda leer.
- La imagen se construye en varias etapas: Node compila la SPA, Go compila el binario y distroless lo ejecuta.

## Alternativas descartadas

- SSE o WebSocket: el polling funciona con la cookie de sesión tal cual y basta para una ejecución de un segundo.
- Servir la SPA desde otro contenedor: obliga a configurar CORS y añade un servicio.

## Consecuencias

- Estado: la API con polling está hecha. La SPA se embebe y se añade la etapa de Node al Dockerfile cuando exista la interfaz.
- Cada cambio del front obliga a reconstruir el binario. En desarrollo Vite corre aparte con proxy a la API.
