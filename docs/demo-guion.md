# Guion de la demo

Unos 9 minutos y medio, grabados, dentro del límite de 10. Cada escena dice qué se ve, qué se hace y qué se cuenta. Las cifras son las del dataset de la prueba y salen tal cual en pantalla.

## Antes de grabar

1. La aplicación tiene que estar sin análisis. Si ya se ejecutó uno, borra el estado:
   - En la instancia de AWS: `AWS_PROFILE=<perfil> ./deploy/aws/reset-demo.sh`. Borra análisis, anomalías y acciones, y deja las explicaciones de Gemini en la caché.
   - En local: `docker exec voltia-postgres-1 psql -U voltia -d voltia -c "delete from anomaly_actions; delete from anomalies; delete from analysis_runs"`.
2. La caché de Gemini debe tener las tres explicaciones. Se comprueba ejecutando un análisis de ensayo: las tres anomalías (M-109, M-112, M-104) tienen que decir "Redactado por gemini-3.8-flash". Si alguna dice "Texto de plantilla", Google respondió con cuota agotada o saturación. Espera unos minutos y repite, o graba igual y cuenta que la plantilla es el respaldo previsto. Después del ensayo, vuelve a borrar el estado.
3. Abre la aplicación en modo oscuro, ventana de 1280 px de ancho, sin otras pestañas salvo la de la arquitectura del paso 5.
4. El formulario de entrada sale vacío. Se entra con el botón "Entrar como demo". Si prefieres escribirlo a mano, la cuenta es `demo@voltia.local` con la contraseña `voltia-demo-2026`.
5. Deja abierta en otra pestaña la arquitectura: `https://github.com/m0ntbl4ck/voltia/blob/main/docs/architecture.md#3-vista-de-contexto-y-contenedores`. GitHub dibuja el diagrama solo.

## Escenas

| # | Tiempo | Pantalla | Qué se hace | Qué se cuenta |
|---|---|---|---|---|
| 1 | 0:00 a 0:40 | Login | Se muestra el formulario vacío y se pulsa "Entrar como demo" | VoltIA: volt de voltio, IA de inteligencia artificial. Lee lecturas de 12 medidores eléctricos y dice cuáles investigar primero. Una idea guía el diseño: el motor decide con reglas que se pueden probar, y el modelo de lenguaje solo redacta lo que el motor ya calculó. |
| 2 | 0:40 a 1:00 | Dashboard vacío | No se toca nada | Todavía no hay análisis, y la pantalla lo dice y ofrece la acción. Este es el "antes". |
| 3 | 1:00 a 1:40 | Panel del análisis | Se pulsa "Ejecutar análisis" y se deja correr. En pantalla dura unos 4 segundos, así que las etapas se explican antes de pulsar o al terminar, mirando el panel | Siete etapas: lecturas, baseline, detección, correlación, eventos, explicación y recomendación. El baseline es lo que cada medidor hace normalmente a cada hora. Al terminar: 4 anomalías, 2 de severidad alta. Se cierra el panel con Escape. |
| 4 | 1:40 a 2:40 | Dashboard lleno | Se recorren los indicadores, la lista de atención y el mapa de calor | Hay 2 altas prioridades pendientes. En el mapa se ve M-109 en rojo desde el 12 de septiembre y M-104 desde el 11. M-106 tiene una sola casilla que baja el 8. M-112 no aparece porque su problema no cambia el consumo: se ve en las lecturas eléctricas. |
| 5 | 2:40 a 3:30 | Medidores y detalle de M-109 | Se ordena por variación, se abre M-109, se cambia a Corriente y a Factor de potencia, y se abre la tabla por día | La banda gris es lo esperado a esa hora. Desde el 12 de septiembre a las 14:00 el consumo sube un 110 %, la corriente se duplica y el factor de potencia baja de 0,94 a 0,74. Las filas de la tabla se colorean desde ese día. Hay un evento reportado, pero sin causa conocida. |
| 6 | 3:30 a 5:30 | Anomalías IA e investigación de M-109 | Se abre la primera fila. Se recorre el texto, "Antes y ahora", los eventos, el desglose de la confianza y el de la prioridad | Prioridad 100 y confianza 0,99. El texto lo redactó `gemini-3.8-flash` y lo dice la insignia. El motor ya había decidido el tipo, la severidad y las cifras, y una guarda rechaza cualquier número que el modelo escriba y no esté en la evidencia. Si el modelo falla, el operador lee una plantilla. |
| 7 | 5:30 a 6:30 | Acción | Se pulsa "Crear orden de inspección", se escribe una nota, se confirma. Se vuelve al dashboard | La acción pide confirmación y queda en el historial con quién y cuándo. La anomalía pasa a reconocida y las altas prioridades pendientes bajan de 2 a 1. |
| 8 | 6:30 a 7:10 | M-112 | Se abre desde la lista | Es calidad de datos, no una avería: 16 lecturas eléctricas incoherentes en 46 horas mientras el consumo es estable. La acción es pedir la validación del medidor, no mandar una cuadrilla. |
| 9 | 7:10 a 7:40 | M-106 | Se abre y se muestra el evento | Prioridad 5. La caída del 8 de septiembre coincide con una parada programada de 12 horas, así que es un falso positivo y se descarta. |
| 10 | 7:40 a 9:20 | Arquitectura | Se abre el diagrama de la sección 3 de `docs/architecture.md` en GitHub y, al final, `/api/docs` | Ver el detalle debajo de la tabla. |

## Escena 10 en detalle: la arquitectura (unos 100 segundos)

Se muestra el diagrama y se cuenta en este orden, sin abrir código:

1. **El flujo.** Datos, análisis, anomalía, explicación, priorización y acción. Es el recorrido que acaba de verse en pantalla.
2. **El principio.** El motor determinista decide el tipo, la severidad, la prioridad y la confianza. El modelo de lenguaje solo redacta lo que el motor ya calculó.
3. **La protección.** Una guarda rechaza cualquier número que no esté en la evidencia. Si Gemini falla o inventa algo, el operador lee una plantilla. Durante las pruebas pasó de verdad: el modelo copió fechas en formato ISO, la guarda las rechazó y se ajustó el prompt.
4. **El backend.** Go con arquitectura hexagonal, PostgreSQL con migraciones, y la API documentada con OpenAPI. Se abre `/api/docs` unos segundos.
5. **El frontend y el despliegue.** React en otro repositorio, alojado en Amplify. La API corre en una instancia de AWS con HTTPS, y una regla reescribe `/api` para que el navegador vea un solo origen.
6. **Las decisiones.** Están escritas en los ADRs de `docs/adr/`, con sus alternativas descartadas. Se menciona en una frase.

## Ensayo

El 26 de septiembre se recorrió este guion completo contra la aplicación desplegada en Amplify, con un navegador limpio, y se comprobó cada cifra que se dice en las escenas: los 4 resultados del análisis, las 2 altas prioridades que bajan a 1 tras la acción, las prioridades 100, 65, 53 y 5, la confianza de 99 %, el origen del texto en cada anomalía, los días marcados en el mapa de calor y en la tabla por día, y la documentación de la API. Todo coincidió y no hubo errores de página ni respuestas 5xx. El recorrido automático dura unos 20 segundos, sin las pausas de narración.

## Límites que conviene decir en voz alta

- La confianza mide la solidez de la evidencia. No es una probabilidad calibrada, porque no hay datos etiquetados.
- La pantalla de análisis muestra el último análisis, no un historial.
- El estado es compartido: cualquiera que entre con la cuenta pública puede ejecutar análisis y aplicar acciones.
- La cuota gratuita de Gemini se agota con las pruebas. Con la caché, repetir un análisis no vuelve a llamar al modelo.
