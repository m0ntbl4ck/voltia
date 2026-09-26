# Despliegue en AWS

Una sola instancia EC2 corre PostgreSQL, la API y Caddy. Caddy consigue su certificado HTTPS, sirve el frontend compilado y reenvía `/api` a la API. Las razones y los límites están en el [ADR 0011](../../docs/adr/0011-despliegue-en-una-instancia-ec2.md).

## Qué se crea

Todo lleva la etiqueta `project=voltia`, en `us-east-1`:

| Recurso | Detalle |
|---|---|
| Instancia EC2 | `t3.small`, Amazon Linux 2023, disco gp3 de 20 GB, IMDSv2 obligatorio |
| IP elástica | Fija el nombre `A-B-C-D.sslip.io` con el que Caddy pide el certificado |
| Grupo de seguridad | Entrada solo por 80 y 443. No hay puerto 22: se entra por Session Manager |
| Rol de instancia | `AmazonSSMManagedInstanceCore` más lectura de `/voltia/*` en Parameter Store |
| Parámetros cifrados | `/voltia/JWT_SECRET`, `/voltia/POSTGRES_PASSWORD`, `/voltia/GEMINI_API_KEY` |

Costo aproximado con la instancia encendida: unos 20 USD al mes. Confírmalo con el estimador de costos de AWS antes de crear nada.

## Crear

1. Crea el rol y el perfil de instancia, y guarda los tres parámetros con `aws ssm put-parameter --type SecureString`.
2. Crea el grupo de seguridad con entrada 80/tcp, 443/tcp y 443/udp, y reserva una IP elástica.
3. Sustituye `__REGION__` y `__SITE_ADDRESS__` en `user-data.sh` (por ejemplo `34-196-32-65.sslip.io`).
4. Lanza la instancia con ese script como `user-data` y asocia la IP elástica.

El script instala Docker, Compose y buildx, clona los dos repositorios, compila el frontend en un contenedor de Node y levanta `compose.yml`. El registro queda en `/var/log/voltia-bootstrap.log`.

## Actualizar

Por Session Manager, en la instancia:

```sh
cd /opt/voltia && git pull
cd /opt/voltia-web && git pull   # y volver a compilar dist/ como hace user-data.sh
cd /opt/deploy && docker compose up -d --build
```

## Borrar todo

Busca los recursos por la etiqueta y bórralos en este orden: instancia (`terminate-instances`), IP elástica (`release-address`), grupo de seguridad, perfil de instancia y rol, y los tres parámetros de `/voltia/`. Mientras exista la IP elástica sin instancia asociada, AWS la cobra.
