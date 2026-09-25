# ADR 0002: PostgreSQL, sqlc y goose; estado del medidor derivado

Estado: aceptada. Fecha: 2026-09-25.

## Contexto

Los datos son lecturas horarias por medidor, anomalías que se actualizan por huella entre ejecuciones y un historial de acciones que no puede perder ni duplicar transiciones.

## Decisión

- PostgreSQL 17 con `pgx` a través de `database/sql`.
- `sqlc` genera el código de las consultas desde `internal/adapters/postgres/queries/*.sql`.
- `goose` se usa como librería: las migraciones van embebidas en el binario y se aplican al arrancar.
- El seed es idempotente (`ON CONFLICT DO NOTHING`): arrancar dos veces no duplica nada.
- Una acción sobre una anomalía bloquea su fila (`SELECT ... FOR UPDATE`) antes de cambiar el estado y escribir el historial.
- El estado del medidor (OK, Alert, Critical) no se guarda. Lo calcula `domain.StatusOf` a partir de las anomalías sin resolver.

## Alternativas descartadas

- Un ORM: esconde el SQL y no aporta nada con este esquema.
- Guardar el estado del medidor: puede quedar desincronizado de sus anomalías. Derivarlo lo hace imposible.
- SQLite: alcanzaría para los datos, pero el compose ya levanta Postgres y el bloqueo de fila es la forma directa de serializar acciones concurrentes.

## Consecuencias

- Las consultas están tipadas y el compilador avisa si el esquema y el SQL se separan.
- Los tests de la capa Postgres usan una base real en un esquema desechable, y se saltan sin `DATABASE_URL`.
- Cada petición de medidores calcula el estado y reconstruye el baseline desde las lecturas. Con 12 medidores y 14 días es inmediato; a escala habría que guardarlo con el análisis.
