# Voltia

Plataforma de gestión energética para la prueba técnica AI Energy Management. Detecta anomalías en medidores eléctricos, las clasifica, las prioriza y explica cada una con la evidencia que la sustenta.

## Estado

En construcción. Hoy existe la estructura del proyecto: un servidor Go con `/healthz`, una app de Vite con React y una base de datos PostgreSQL en Docker. El motor de análisis, la API y las pantallas todavía no están escritos.

El diseño completo está en [docs/architecture.md](docs/architecture.md).

## Requisitos

Go 1.27, Node 24 y Docker con Compose.

## Uso local

```sh
cp .env.example .env
make db        # levanta PostgreSQL
make dev-api   # servidor Go en :8080
make dev-web   # Vite en :5173, con /api dirigido al servidor Go
make test
make lint
```

## Datos

`data/readings.csv` tiene 4.032 lecturas horarias de 12 medidores durante 14 días. `data/events.csv` tiene los 4 eventos operativos conocidos.
