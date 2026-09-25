# ADR 0004: Baseline horario robusto: mediana y MAD con referencia excluyente

Estado: aceptada. Fecha: 2026-09-25.

## Contexto

Hay que decidir qué es normal para cada medidor antes de llamar anómala a una lectura. Los datos tienen paradas, picos y una semana entera de comportamiento distinto dentro de 14 días.

## Decisión

- Un perfil de 24 horas por medidor y por variable (kWh, V, A, FP), con la mediana como centro y `sigma = max(1,4826 * MAD, piso * |mediana|)`.
- El piso de sigma depende de la variable: 5% de la mediana en consumo y corriente, 2% en factor de potencia y 0,5% en voltaje. Un piso único del 5% dejaba pasar un salto de 241 V con z = 1,9.
- La referencia son los primeros 7 días de cada medidor. El análisis recorre solo las lecturas posteriores.
- Las lecturas cubiertas por un evento reportado quedan fuera del baseline, salvo los eventos `UNKNOWN`. Duran lo que el evento declare ("for N hours") o 24 h si no declara nada.

## Alternativas descartadas

- Media y desviación estándar: una parada o un pico desplaza el baseline y esconde el siguiente.
- Un baseline global por hora del día: los medidores no se parecen entre sí.

## Consecuencias

- El baseline no se persiste. Cada anomalía guarda en su evidencia los valores que usó.
- Con 7 días de referencia no se separan días laborales de fines de semana.
- Las 24 h para eventos sin duración son un valor sin respaldo en los datos.
