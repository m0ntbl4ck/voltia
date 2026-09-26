#!/bin/bash
# Amazon Linux 2023 boot script. Secrets come from SSM Parameter Store, never
# from this file. The two values below are filled in when the instance is created.
set -euxo pipefail
exec > >(tee /var/log/voltia-bootstrap.log) 2>&1

REGION="__REGION__"
SITE_ADDRESS="__SITE_ADDRESS__"
DEMO_EMAIL="demo@voltia.local"
DEMO_PASSWORD="voltia-demo-2026"

dnf install -y docker git
systemctl enable --now docker
mkdir -p /usr/local/lib/docker/cli-plugins
curl -fsSL https://github.com/docker/compose/releases/latest/download/docker-compose-linux-x86_64 \
  -o /usr/local/lib/docker/cli-plugins/docker-compose
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose
# Amazon Linux ships an old buildx, and compose build needs 0.17 or later.
BUILDX="$(curl -fsSL https://api.github.com/repos/docker/buildx/releases/latest | grep -m1 '"tag_name"' | cut -d'"' -f4)"
curl -fsSL "https://github.com/docker/buildx/releases/download/${BUILDX}/buildx-${BUILDX}.linux-amd64" \
  -o /usr/local/lib/docker/cli-plugins/docker-buildx
chmod +x /usr/local/lib/docker/cli-plugins/docker-buildx

param() { aws ssm get-parameter --region "$REGION" --with-decryption --name "/voltia/$1" --query Parameter.Value --output text; }
JWT_SECRET="$(param JWT_SECRET)"
GEMINI_API_KEY="$(param GEMINI_API_KEY)"
POSTGRES_PASSWORD="$(param POSTGRES_PASSWORD)"

git clone --depth 1 https://github.com/m0ntbl4ck/voltia.git /opt/voltia
git clone --depth 1 https://github.com/m0ntbl4ck/voltia-web.git /opt/voltia-web

# Build the frontend in a throwaway container so the instance needs no Node.
docker run --rm -v /opt/voltia-web:/app -w /app \
  -e VITE_DEMO_EMAIL="$DEMO_EMAIL" -e VITE_DEMO_PASSWORD="$DEMO_PASSWORD" \
  node:24-alpine sh -c "npm ci && npm run build"
mkdir -p /opt/site && rm -rf /opt/site/web && cp -r /opt/voltia-web/dist /opt/site/web

mkdir -p /opt/deploy
cp /opt/voltia/deploy/aws/compose.yml /opt/voltia/deploy/aws/Caddyfile /opt/deploy/ 2>/dev/null || true
umask 077
cat > /opt/deploy/.env <<ENVFILE
SITE_ADDRESS=$SITE_ADDRESS
DEMO_EMAIL=$DEMO_EMAIL
DEMO_PASSWORD=$DEMO_PASSWORD
JWT_SECRET=$JWT_SECRET
GEMINI_API_KEY=$GEMINI_API_KEY
POSTGRES_PASSWORD=$POSTGRES_PASSWORD
ENVFILE

cd /opt/deploy && docker compose up -d --build
echo "bootstrap finished"
