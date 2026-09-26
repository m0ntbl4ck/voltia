# Discurso de la demo

Texto para decir en voz alta, de principio a fin: primero qué es la aplicación, luego la arquitectura y al final el recorrido de la demo. Son unas 1.100 palabras, unos 8 minutos hablando con calma, más las pausas mientras haces clic y carga cada pantalla. Los paréntesis marcan cuándo cambias de pantalla; no se leen. Las escenas y sus tiempos están en [`demo-guion.md`](demo-guion.md).

## Parte 1. Qué es la aplicación

Hola. Les presento VoltIA. El nombre junta dos cosas: volt, de voltio, e IA, de inteligencia artificial.

VoltIA lee las lecturas de los medidores eléctricos de una planta industrial. En esta prueba son doce medidores y catorce días de datos, hora por hora. Con eso responde una pregunta que un jefe de mantenimiento se hace todos los días: ¿qué medidor reviso primero, y por qué?

Para contestarla hace cuatro cosas. Primero aprende cómo se comporta cada medidor en cada hora del día. Después marca lo que se sale de ese comportamiento. Luego decide qué es: una avería real, un cambio que se explica por algo que pasó en la planta, un falso positivo, o un problema del propio medidor. Y por último las ordena por prioridad, con una explicación que un operador entiende y una acción concreta que puede tomar ahí mismo.

Un ejemplo con los datos de la prueba. El molino, el medidor M-109, empezó a consumir un 110 % más de lo normal el 12 de septiembre a las dos de la tarde. Nadie reportó nada que lo explique. VoltIA lo pone de primero, con prioridad 100, y recomienda abrir una orden de inspección.

## Parte 2. La arquitectura

La arquitectura tiene una idea central: el motor decide y el modelo de lenguaje solo redacta.

Todo lo que importa lo calculan reglas escritas en Go: qué tipo de anomalía es, qué tan grave, qué prioridad tiene y qué tan sólida es la evidencia. Dan el mismo resultado cada vez y se pueden probar. Hay un test de regresión con los cuatro casos del dataset, con valores calculados aparte, para que el test no se compare contra el mismo código que prueba.

Gemini entra al final. Recibe la evidencia ya calculada y escribe la explicación en español. No puede cambiar el tipo, la severidad ni las cifras. Además hay una guarda: si el texto trae un número que no está en la evidencia, se descarta y el operador lee una plantilla. Esto pasó de verdad. En las pruebas, el modelo copió fechas en formato ISO, la guarda las rechazó y tuve que ajustar el prompt. Si Google está saturado o se acaba la cuota, pasa lo mismo: plantilla, y la aplicación sigue funcionando.

El backend es un monolito en Go con arquitectura hexagonal. El motor no conoce la base de datos, ni el HTTP, ni el modelo. Guarda todo en PostgreSQL, y la API está documentada con OpenAPI, con Swagger incluido.

El frontend es una aplicación en React, en un repositorio aparte que se llama voltia-web. Separé los dos repositorios a propósito, para que cada parte se despliegue y cambie sin arrastrar a la otra.

Y todo está desplegado. La API corre en una instancia de AWS con HTTPS, y el frontend está en AWS Amplify. Amplify reenvía las llamadas a la API, así que para el navegador todo es un solo dominio y la cookie de sesión funciona sin configurar CORS. Cada decisión está escrita en los ADRs del repositorio, con las alternativas que descarté y por qué.

## Parte 3. La demo

(Abres la aplicación en el navegador.)

Paso a la demo. Entro con la cuenta de demostración.

Lo primero que veo es que todavía no hay análisis. La pantalla me lo dice y me ofrece ejecutarlo. Este es el estado inicial.

(Pulsas "Ejecutar análisis".)

Ejecuto el análisis. Se ven las siete etapas: lecturas, baseline, detección, correlación, eventos, explicación y recomendación. El baseline es lo que cada medidor hace normalmente a cada hora. Termina en unos segundos: cuatro anomalías, dos de severidad alta.

(Cierras el panel y miras el dashboard.)

Ahora el dashboard. Hay dos altas prioridades pendientes. Más abajo está la lista de lo que requiere atención, con las tres más urgentes. Y este mapa de calor: cada fila es un medidor, cada columna un día, y cada casilla dice cuánto se desvió ese día de lo que ese medidor consume normalmente. Se ve el M-109 marcado desde el día 12, el M-104 desde el 11 y el M-106 con una sola casilla, el día 8. El M-112 no aparece aquí, y en un momento explico por qué.

(Vas a Medidores, ordenas por variación y abres M-109.)

En Medidores ordeno por variación y el M-109 queda arriba. Abro su detalle. La banda gris es lo esperado a esa hora, y la línea se sale el 12 de septiembre a las dos de la tarde. El consumo sube un 110 %, la corriente pasa de unos 200 a unos 420 amperios y el factor de potencia baja de 0,94 a 0,74. En la tabla por día, los días afectados salen marcados con el color de la anomalía. Ese día hay un evento reportado, pero no dice nada sobre la causa.

(Vas a Anomalías IA y abres la primera.)

En Anomalías IA están ordenadas por prioridad: 100, 65, 53 y 5. Abro la primera. El texto lo redactó Gemini, y la insignia lo dice. El motor ya había decidido el tipo, la severidad y las cifras. Más abajo están el antes y el ahora de cada variable, los eventos, la evidencia de cada detector y los desgloses. La confianza es del 99 %, y hay que leerla como solidez de la evidencia, no como una probabilidad calibrada, porque no tengo datos etiquetados para calibrarla. La prioridad suma la severidad, el tipo, el impacto en kilovatios hora y que el episodio sigue en curso.

(Creas la orden de inspección.)

Ahora actúo. Creo la orden de inspección y escribo una nota. La aplicación pide confirmación, la anomalía pasa a reconocida y el historial guarda quién lo hizo y cuándo.

(Vuelves al dashboard.)

Vuelvo al dashboard: las altas prioridades pendientes bajaron de dos a una.

(Abres M-112.)

El M-112 es calidad de datos. Su consumo está estable, pero tiene dieciséis lecturas eléctricas incoherentes en cuarenta y seis horas. Aquí no mando una cuadrilla: pido validar el medidor.

(Abres M-106.)

Y el M-106 es un falso positivo. Tuvo una caída de doce horas el 8 de septiembre, igual a una parada programada. Prioridad 5, y la acción es descartarlo. Su texto sale de una plantilla, porque los falsos positivos no necesitan llamar al modelo.

(Abres la documentación de la API en `/api/docs`.)

Con esto respondo la pregunta del principio: qué revisar primero, por qué, y qué hacer. La API está documentada y se puede probar desde aquí.

Antes de cerrar, lo que falta. No escribí el adaptador para Claude, aunque el sistema lo admite. La aplicación muestra solo el último análisis, sin historial. Y el despliegue es de demostración: una sola instancia, sin copias de seguridad y con una cuenta pública. Gracias.
