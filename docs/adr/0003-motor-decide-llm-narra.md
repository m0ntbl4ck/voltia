# ADR 0003: El motor decide, el LLM narra, las plantillas respaldan

Estado: aceptada. Fecha: 2026-09-25.

## Contexto

Una explicación sobre una anomalía eléctrica tiene que ser reproducible y no puede inventar datos. Además, el sistema debe funcionar sin clave de API y cuando el proveedor falla o se queda sin cuota.

## Decisión

- Tipo, severidad, prioridad y confianza los calculan reglas deterministas. El modelo nunca los ve como una pregunta: los recibe como hechos.
- El modelo solo recibe el paquete de evidencia estructurada y devuelve `summary`, `reason`, `recommended_action` e `investigation_steps` en JSON con esquema.
- Una guarda revisa cada número del texto: debe estar en la evidencia, con tolerancia de redondeo, y las horas se comprueban en la zona horaria de la planta. Si un número no aparece, se descarta el texto y se usa la plantilla.
- La plantilla también se usa si falla la llamada, si pasan 20 s, si falta la clave, y directamente para falsos positivos y severidad baja.
- El texto del modelo se guarda en `explanation_cache` por hash de la evidencia, el proveedor, el modelo y la versión del prompt. El texto de plantilla no se guarda, para que la siguiente ejecución vuelva a intentar con el modelo.

## Alternativas descartadas

- Que el modelo clasifique: no es reproducible ni se puede probar con un test de regresión.
- Solo plantillas: el texto sale rígido y no relaciona la evidencia con lo que ve el operador.

## Consecuencias

- Reejecutar el análisis da los mismos resultados y, con caché, el mismo texto.
- La guarda solo comprueba números. Una causa inventada que no lleve cifras pasaría, y el prompt es la única barrera contra eso.
- La guarda puede rechazar un texto correcto que redondee de una forma que no prevé, y en ese caso el operador ve la plantilla.
- La interfaz debe mostrar qué escribió cada explicación (motor y modelo, o plantilla).
