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
- Con tres fuentes la coincidencia de detectores llegaba a 1 con un incremento de 0,25 por fuente, y tres de los cuatro casos del dataset quedaban con la misma confianza (0,99). El 27 de septiembre se bajó el incremento a 0,15 por fuente: hacen falta cinco fuentes para tocar el techo, y los tres casos se separan según su evidencia (M-106 en 0,98, M-104 en 0,93, M-112 en 0,92). M-109 no cambia porque ya tenía fuentes de sobra sin necesitar a D7.
