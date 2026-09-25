# ADR 0005: Detectores por reglas, con Isolation Forest como corroborador

Estado: aceptada. Fecha: 2026-09-25.

## Contexto

No hay datos etiquetados, así que no se puede entrenar un clasificador supervisado. Cada anomalía tiene que llegar al operador con la evidencia que la sustenta.

## Decisión

- Seis detectores por reglas (D1 a D6): cambio persistente, pico o caída, outlier, patrón horario, relación eléctrica y calidad de datos. Cada uno emite señales con su evidencia y ninguno decide el tipo.
- D7 es un Isolation Forest propio en Go (500 árboles, muestra de 256, semilla 7), entrenado por medidor con su semana de referencia sobre `[z_kWh, z_V, z_I, z_FP, log(ratio_físico)]`. Corrobora un episodio que ya formaron los otros detectores, suma una fuente a la confianza y entrega la atribución por variable. Nunca abre un episodio ni cambia tipo, severidad o prioridad.

## Alternativas descartadas

- Solo Isolation Forest: puntúa, pero no explica ni clasifica.
- Solo reglas: no hay una segunda opinión independiente sobre el mismo episodio.

## Consecuencias

- Se validó contra una implementación independiente en numpy (2000 árboles) sobre los mismos datos. Coinciden en las puntuaciones máximas y en las variables que aíslan, no en cifras exactas porque los generadores aleatorios difieren.
- Los medidores sanos también puntúan alto en algunas horas (hasta 9 de 168). Por eso corrobora por proporción de horas y no por una sola.
- Con tres fuentes la coincidencia de detectores ya llega a 1, y tres de los cuatro casos quedan con confianza 0,99. Hay que decidir si la fórmula debe saturar.
