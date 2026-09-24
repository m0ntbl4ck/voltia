# Voltia ⚡

Plataforma de gestión energética con IA: detecta anomalías en medidores eléctricos, las clasifica, las prioriza, explica con evidencia por qué y recomienda la siguiente acción.

> 🚧 En construcción. El diseño completo está en [docs/architecture.md](docs/architecture.md).

## Stack

Go (chi, PostgreSQL, sqlc) · React + Vite + TypeScript · ECharts · Gemini 3.8 Flash para las explicaciones.

## Datos

`data/readings.csv` (4.032 lecturas horarias de 12 medidores durante 14 días) y `data/events.csv` (eventos operativos conocidos).
