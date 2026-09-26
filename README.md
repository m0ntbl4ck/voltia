# Voltia

Plataforma de gestión energética para la prueba técnica AI Energy Management. Lee las lecturas de 12 medidores eléctricos, detecta lo que se sale de su comportamiento normal, decide si es una avería real, un cambio explicable, un falso positivo o un problema de datos, las ordena por prioridad y explica cada una con la evidencia que la sustenta.

El motor analítico decide y el modelo de lenguaje solo redacta. Detección, clasificación, severidad, prioridad y confianza son reglas deterministas que se pueden probar. Gemini recibe la evidencia ya calculada y escribe la explicación. Si no hay clave, falla o inventa un número, el operador lee una plantilla.

## Estado

El servidor está completo: motor, análisis asíncrono, autenticación, anomalías con su ciclo de acciones, medidores, dashboard y documentación OpenAPI.

La interfaz vive en otro repositorio, [`voltia-web`](https://github.com/m0ntbl4ck/voltia-web) (React con Vite), y consume esta API. Aquí solo se dockeriza el servidor. Para verla: levanta el servidor con `docker compose up` y, en `voltia-web`, corre `npm install` y `npm run dev`. Tampoco está el adaptador de Claude: `LLM_PROVIDER` acepta `gemini` o `template`. El adaptador de Gemini se probó contra un servidor local, no contra la API real.

## Arranque

Requisitos: Docker con Compose.

```sh
cp .env.example .env
```

Edita `.env` y completa:

- `JWT_SECRET`: al menos 32 caracteres. Genera uno con `openssl rand -hex 32`.
- `DEMO_EMAIL` y `DEMO_PASSWORD`: la cuenta con la que vas a entrar. Se crea o se actualiza en cada arranque.
- `GEMINI_API_KEY`: opcional. Sin ella las explicaciones salen de plantillas.

Luego:

```sh
make up
```

Levanta PostgreSQL y el servidor en `http://localhost:8080`. Al primer arranque migra el esquema y siembra 12 medidores, 4.032 lecturas y 4 eventos. La documentación interactiva de la API está en `http://localhost:8080/api/docs`.

Para probar el ciclo completo desde la terminal:

```sh
curl -c cookies -X POST localhost:8080/api/v1/auth/login \
  -H 'content-type: application/json' \
  -d '{"email":"<DEMO_EMAIL>","password":"<DEMO_PASSWORD>"}'
curl -b cookies -X POST localhost:8080/api/v1/ai/analyze
curl -b cookies localhost:8080/api/v1/ai/analysis/latest
curl -b cookies 'localhost:8080/api/v1/anomalies'
```

`make down` apaga todo y conserva los datos. Para empezar de cero, `docker compose down -v`.

## Qué encuentra en los datos

El análisis del dataset entregado produce cuatro anomalías. Un test de regresión lo verifica, con valores calculados aparte y no copiados del código.

| Medidor | Qué pasa | Tipo | Severidad | Prioridad |
|---|---|---|---|---|
| M-109 | Consumo +110% desde el 12 de septiembre a las 14:00, corriente al doble y factor de potencia de 0,94 a 0,73. No hay evento que lo explique | Anomalía real | Alta | 100 |
| M-112 | Consumo estable, pero 16 lecturas con voltaje y factor de potencia físicamente incoherentes | Calidad de datos | Alta | 65 |
| M-104 | Consumo +47% desde el 11 de septiembre, con el factor de potencia estable. Coincide con una nueva línea productiva | Anomalía explicable | Media | 53 |
| M-106 | Caída de 12 horas el 8 de septiembre, igual a una parada programada | Falso positivo | Baja | 5 |

Los otros 8 medidores no tienen desviaciones. La confianza es un índice de solidez de la evidencia, no una probabilidad calibrada: no hay datos etiquetados con los que calibrarla.

## Configuración

Todo se lee del entorno. `.env.example` lista las variables con su explicación.

| Variable | Por defecto | Uso |
|---|---|---|
| `DATABASE_URL` | obligatoria | Conexión a PostgreSQL. El compose ya la define |
| `JWT_SECRET` | obligatoria | Firma la cookie de sesión |
| `DEMO_EMAIL`, `DEMO_PASSWORD`, `DEMO_NAME` | vacías, `Demo` | Cuenta que se crea al arrancar. Van las dos primeras juntas o ninguna |
| `PORT` | `8080` | Puerto del servidor |
| `PLANT_TZ` | `America/Bogota` | Zona horaria de los CSV y de los textos |
| `SESSION_TTL` | `12h` | Duración de la sesión |
| `LLM_PROVIDER` | `gemini` | `gemini` o `template` |
| `LLM_MODEL` | `gemini-3.8-flash` | Modelo de Gemini |
| `GEMINI_API_KEY` | vacía | Sin ella se usan plantillas |
| `ANALYSIS_STAGE_DELAY` | `300ms` | Pausa tras cada etapa para que el avance se lea. `0s` la quita |

## API

Base `/api/v1`, JSON en snake_case, errores `application/problem+json`. Todo requiere sesión salvo el login y el logout. La referencia completa está en [`api/openapi.yaml`](api/openapi.yaml) y en `/api/docs`.

| Método | Ruta | Para qué |
|---|---|---|
| POST | `/auth/login`, `/auth/logout` | Abre y cierra la sesión (cookie `HttpOnly`) |
| GET | `/auth/me` | Usuario actual |
| GET | `/dashboard/summary` | 6 KPIs, consumo diario, último análisis y las 3 anomalías abiertas más urgentes |
| GET | `/meters` | Medidores con estado, filtros, búsqueda y orden |
| GET | `/meters/{id}`, `/meters/{id}/readings`, `/meters/{id}/events` | Detalle, lecturas con su baseline y eventos |
| POST | `/ai/analyze` | Lanza un análisis. Responde 202 con su id |
| GET | `/ai/analysis/{id}`, `/ai/analysis/latest` | Etapa, progreso y resumen |
| GET | `/anomalies`, `/anomalies/{id}` | Lista por prioridad, y detalle con evidencia, desgloses e historial |
| POST | `/anomalies/{id}/actions` | Crear orden de inspección, pedir validación del medidor, confirmar la operación, resolver o descartar |
| GET | `/healthz` | Salud |

## Desarrollo local

Requisitos: Go 1.27 y Docker. Para la interfaz, ver [`voltia-web`](https://github.com/m0ntbl4ck/voltia-web).

```sh
cp .env.example .env
make db        # solo PostgreSQL
make dev-api   # servidor Go en :8080
make test
make lint
make sqlc      # regenera el código de las consultas
```

Los tests de la capa de Postgres se saltan sin `DATABASE_URL`. El Makefile carga `.env`, así que con la base arriba (`make db`), `make test` los corre.

## Estructura

```
cmd/voltia/          arranque: configuración, migraciones, seed y servidor
internal/domain/     entidades y enumeraciones
internal/analysis/   el motor: baseline, detectores, Isolation Forest, clasificación y puntuación
internal/app/        casos de uso
internal/ports/      interfaces que el núcleo necesita del exterior
internal/adapters/   http (chi), postgres (sqlc, goose), llm (Gemini, plantillas, guarda) y seed
api/                 openapi.yaml y Swagger UI
data/                readings.csv, events.csv y meters.yaml
docs/                arquitectura y decisiones (adr/)
```

## Datos

`data/readings.csv` tiene 4.032 lecturas horarias de 12 medidores durante 14 días. `data/events.csv` tiene los 4 eventos operativos conocidos. Los nombres y ubicaciones de `data/meters.yaml` son ficticios. El archivo de resultados esperados del reto no existe en este repositorio ni lo usa el sistema.

## Documentación

- [`docs/architecture.md`](docs/architecture.md): el diseño completo, el motor y sus umbrales, el modelo de datos y las limitaciones conocidas.
- [`docs/adr/`](docs/adr): nueve decisiones, cada una con su contexto, las alternativas descartadas y sus consecuencias.
