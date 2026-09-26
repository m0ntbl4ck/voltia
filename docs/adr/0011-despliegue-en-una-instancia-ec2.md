# ADR 0011: La API en una instancia EC2 con Docker Compose y Caddy, el frontend en Amplify

Estado: aceptada. Fecha: 2026-09-26.

## Contexto

La arquitectura dejó el despliegue en la nube fuera de alcance, y el ADR 0010 separó el frontend del backend. La demo pública necesita una URL con HTTPS que muestre la aplicación completa. La cuenta de AWS elegida pertenece a una organización, así que no tiene créditos del plan gratuito: todo lo que se cree se cobra.

## Decisión

- Una instancia EC2 `t3.small` corre `postgres`, `voltia` y `caddy` con Docker Compose (`deploy/aws/compose.yml`).
- Caddy consigue el certificado con Let's Encrypt para un nombre `A-B-C-D.sslip.io` que apunta a una IP elástica y reenvía `/api` a la API. No sirve el frontend.
- El frontend (`voltia-web`) se aloja en AWS Amplify, con su propia compilación desde su repositorio. Una regla de reescritura `/api/<*>` con estado 200 hacia la instancia hace que el navegador vea un solo origen, así que no hace falta CORS y la cookie `SameSite=Lax` sigue valiendo. Ninguno de los dos repositorios conoce al otro: el contrato es `api/openapi.yaml`.
- Los secretos viven en Parameter Store cifrados y la instancia los lee al arrancar con su rol. No se escriben en el script de arranque.
- No hay puerto SSH abierto. El acceso administrativo es por Session Manager.
- La cuenta de demostración es pública (`demo@voltia.local`) y su contraseña está en el repositorio.

## Alternativas descartadas

- ECS Express Mode con RDS: unos 45 a 55 USD al mes, sobre todo por el balanceador y la base administrada, para una demo de unos días.
- AWS App Runner: pasó a modo de mantenimiento y no admite clientes nuevos.
- Servir el frontend desde la misma instancia: funcionó, pero obligaba al script de arranque a clonar y compilar el segundo repositorio. Acoplaba los dos repos, que el ADR 0010 separó a propósito.
- Frontend en Netlify: equivalente a Amplify, pero habría añadido un proveedor más.
- GitHub Pages: no puede reenviar `/api`, y con dominios distintos habría que cambiar el backend a CORS con credenciales y `SameSite=None`.
- Supabase: solo aporta Postgres, porque no ejecuta un contenedor de Go.

## Consecuencias

- Que la cookie de sesión atraviese la reescritura de Amplify hay que comprobarlo en el primer despliegue. Si no pasara, el respaldo es CORS con credenciales y `SameSite=None; Secure` en el backend.
- Una sola máquina: si cae, cae la API. Los datos están en un volumen del disco de la instancia y no hay copias de seguridad automáticas.
- Cualquiera con la cuenta pública puede ejecutar análisis y aplicar acciones, y el estado es compartido. No hay un endpoint de reinicio: se hace con SQL por Session Manager.
- El servidor no limita peticiones ni fija tiempos máximos de cabecera.
- `sslip.io` es un dominio compartido, así que Let's Encrypt puede limitar los certificados si muchos lo piden a la vez.
- Gemini tiene cuota gratuita limitada. La primera prueba en AWS recibió un 429 y una explicación cayó a plantilla. La caché de explicaciones se puede copiar de otra base con `pg_dump` de `explanation_cache`, porque la clave depende del proveedor, el modelo, la versión del prompt y la evidencia.
- Mientras la instancia y la IP elástica existan, AWS las cobra. `deploy/aws/README.md` lista cómo borrarlo todo.
