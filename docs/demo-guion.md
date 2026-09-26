# Guion de la demo

Entre 7 y 9 minutos, grabados. Cada escena dice qué se ve, qué se hace y qué se cuenta. Las cifras son las del dataset de la prueba y salen tal cual en pantalla.

## Antes de grabar

1. La aplicación tiene que estar sin análisis. Si ya se ejecutó uno, borra el estado:
   - En la instancia de AWS, por Session Manager: `docker exec deploy-postgres-1 psql -U voltia -d voltia -c "delete from anomaly_actions; delete from anomalies; delete from analysis_runs"`.
   - En local: el mismo comando con `docker exec voltia-postgres-1`.
2. La caché de Gemini debe tener las tres explicaciones. Se comprueba ejecutando un análisis de ensayo: las tres anomalías (M-109, M-112, M-104) tienen que decir "Redactado por gemini-3.8-flash". Si alguna dice "Texto de plantilla", Google respondió con cuota agotada o saturación. Espera unos minutos y repite, o graba igual y cuenta que la plantilla es el respaldo previsto. Después del ensayo, vuelve a borrar el estado.
3. Abre la aplicación en modo oscuro, ventana de 1280 px de ancho, sin otras pestañas.
4. Ten a mano el correo y la contraseña de la cuenta de demostración por si el formulario no sale relleno: `demo@voltia.local` y `voltia-demo-2026`.

## Escenas

| # | Tiempo | Pantalla | Qué se hace | Qué se cuenta |
|---|---|---|---|---|
| 1 | 0:00 a 0:40 | Login | Se muestra el formulario y se pulsa "Entrar como demo" | VoltIA: volt de voltio, IA de inteligencia artificial. Lee lecturas de 12 medidores eléctricos y dice cuáles investigar primero. Una idea guía el diseño: el motor decide con reglas que se pueden probar, y el modelo de lenguaje solo redacta lo que el motor ya calculó. |
| 2 | 0:40 a 1:00 | Dashboard vacío | No se toca nada | Todavía no hay análisis, y la pantalla lo dice y ofrece la acción. Este es el "antes". |
| 3 | 1:00 a 2:00 | Panel del análisis | Se pulsa "Ejecutar análisis" y se deja correr | Siete etapas: lecturas, baseline, detección, correlación, eventos, explicación y recomendación. El baseline es lo que cada medidor hace normalmente a cada hora. Al terminar: 4 anomalías, 2 de severidad alta. Se cierra el panel con Escape. |
| 4 | 2:00 a 3:00 | Dashboard lleno | Se recorren los indicadores, la lista de atención y el mapa de calor | Hay 2 altas prioridades pendientes. En el mapa se ve M-109 en rojo desde el 12 de septiembre y M-104 desde el 11. M-106 tiene una sola casilla que baja el 8. M-112 no aparece porque su problema no cambia el consumo: se ve en las lecturas eléctricas. |
| 5 | 3:00 a 4:00 | Medidores y detalle de M-109 | Se ordena por variación, se abre M-109, se cambia a Corriente y a Factor de potencia, y se abre la tabla por día | La banda gris es lo esperado a esa hora. Desde el 12 de septiembre a las 14:00 el consumo sube un 110 %, la corriente se duplica y el factor de potencia baja de 0,94 a 0,74. Las filas de la tabla se colorean desde ese día. Hay un evento reportado, pero sin causa conocida. |
| 6 | 4:00 a 6:00 | Anomalías IA e investigación de M-109 | Se abre la primera fila. Se recorre el texto, "Antes y ahora", los eventos, el desglose de la confianza y el de la prioridad | Prioridad 100 y confianza 0,99. El texto lo redactó `gemini-3.8-flash` y lo dice la insignia. El motor ya había decidido el tipo, la severidad y las cifras, y una guarda rechaza cualquier número que el modelo escriba y no esté en la evidencia. Si el modelo falla, el operador lee una plantilla. |
| 7 | 6:00 a 7:00 | Acción | Se pulsa "Crear orden de inspección", se escribe una nota, se confirma. Se vuelve al dashboard | La acción pide confirmación y queda en el historial con quién y cuándo. La anomalía pasa a reconocida y las altas prioridades pendientes bajan de 2 a 1. |
| 8 | 7:00 a 7:40 | M-112 | Se abre desde la lista | Es calidad de datos, no una avería: 16 lecturas eléctricas incoherentes en 46 horas mientras el consumo es estable. La acción es pedir la validación del medidor, no mandar una cuadrilla. |
| 9 | 7:40 a 8:10 | M-106 | Se abre y se muestra el evento | Prioridad 5. La caída del 8 de septiembre coincide con una parada programada de 12 horas, así que es un falso positivo y se descarta. |
| 10 | 8:10 a 9:00 | Cierre | Se muestra la documentación de la API (`/api/docs`) o el README | Backend en Go con arquitectura hexagonal, PostgreSQL, y la API documentada con OpenAPI. Frontend en React en otro repositorio. Desplegado en una instancia de AWS con HTTPS y el frontend en Amplify. |

## Límites que conviene decir en voz alta

- La confianza mide la solidez de la evidencia. No es una probabilidad calibrada, porque no hay datos etiquetados.
- La pantalla de análisis muestra el último análisis, no un historial.
- El estado es compartido: cualquiera que entre con la cuenta pública puede ejecutar análisis y aplicar acciones.
- La cuota gratuita de Gemini se agota con las pruebas. Con la caché, repetir un análisis no vuelve a llamar al modelo.
