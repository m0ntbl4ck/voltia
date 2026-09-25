# ADR 0009: snake_case de punta a punta, errores RFC 7807 y contrato verificado

Estado: aceptada. Fecha: 2026-09-25.

## Contexto

La API la consume una SPA y la documentan personas. Los nombres, los errores y el contrato tienen que ser uniformes y no pueden desviarse de la documentación.

## Decisión

- snake_case en la base de datos, en el JSON y en los parámetros de la URL.
- Fechas en ISO-8601 UTC. La zona horaria de la planta (`America/Bogota`) solo aparece en los textos que lee el operador.
- Errores como `application/problem+json` (RFC 7807).
- El contrato está en `api/openapi.yaml`, escrito a mano, con Swagger UI en `/api/docs`. Un test valida cada respuesta del router contra ese esquema y comprueba que las rutas y el documento coinciden.

## Alternativas descartadas

- camelCase en el JSON: sigue la costumbre de JavaScript, pero obliga a convertir nombres en un borde.
- Generar el OpenAPI desde el código: el documento pasa a ser un reflejo de lo que el código hace, no un contrato.

## Consecuencias

- Al añadir o cambiar un endpoint hay que actualizar el YAML y agregar un caso al test de contrato.
- El validador del test es propio: cubre tipos, campos requeridos, `nullable`, enumeraciones, rangos, fechas y claves de más. No implementa todo el estándar JSON Schema.
