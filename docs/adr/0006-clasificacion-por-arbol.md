# ADR 0006: Clasificación por árbol de decisión con reglas de seguridad sobre eventos

Estado: aceptada. Fecha: 2026-09-25.

## Contexto

Los eventos operativos explican parte de lo que parece anómalo, pero uno mal usado esconde una avería real. El clasificador debe poder explicar por qué dio cada tipo.

## Decisión

Un árbol de tres pasos: calidad de datos si hay señales D6 sin cambio persistente; falso positivo o anomalía explicable si hay un evento explicativo compatible; anomalía real en cualquier otro caso. Tres reglas de seguridad:

1. Un evento `UNKNOWN` nunca explica nada. M-109 tiene uno ("No operational event reported") y sigue siendo `REAL_ANOMALY`.
2. Un evento `DATA_QUALITY` solo corrobora; la clasificación sale del chequeo físico.
3. Un evento explicativo no perdona señales eléctricas anómalas (D5): el episodio pasa a `REAL_ANOMALY`.

## Alternativas descartadas

- Puntuar cada tipo con pesos y elegir el mayor: es más difícil de auditar y de probar.
- Un modelo supervisado: no hay etiquetas.

## Consecuencias

- Cada tipo se puede rastrear hasta una rama del árbol.
- Los umbrales están ajustados a este dataset. En producción se calibrarían con el historial y con las acciones de los operadores (DISMISS y RESOLVE).
