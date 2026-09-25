# ADR 0001: Go, monolito modular con hexagonal ligera

Estado: aceptada. Fecha: 2026-09-25.

## Contexto

El reto exige Go. Hay un solo desarrollador, cuatro días y un único despliegue. El motor de análisis debe poder probarse sin base de datos, sin HTTP y sin modelo de lenguaje.

## Decisión

Un solo binario con paquetes separados por responsabilidad:

- `internal/domain`: entidades y enumeraciones.
- `internal/analysis`: el motor, Go puro, sin dependencias de infraestructura.
- `internal/app`: casos de uso (`AnalysisService`, `MeterService`, `DashboardService`, `Auth`).
- `internal/ports`: las interfaces que el núcleo necesita del exterior.
- `internal/adapters`: HTTP (chi), Postgres, LLM y seed, que implementan o consumen los puertos.

## Alternativas descartadas

- Microservicios: un desarrollador y un despliegue no los justifican.
- Capas planas sin puertos: el motor terminaría importando `pgx` y sería difícil de probar.

## Consecuencias

- Los tests del motor corren con datos en memoria y tardan segundos.
- Cambiar de Gemini a Claude o a plantillas no toca el motor ni los casos de uso.
- Cuesta más código: cada dependencia externa lleva su interfaz.
- El análisis corre en una goroutine del mismo proceso. A escala se movería a una cola con un worker, y el puerto `Runs` ya separa el estado de la ejecución.
