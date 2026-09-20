# Stage 1: Base
FROM node:22-bookworm-slim AS base
WORKDIR /app
ENV PNPM_HOME="/pnpm"
ENV PATH="$PNPM_HOME:$PATH"
RUN corepack enable

FROM base AS deps
COPY package.json pnpm-lock.yaml ./
COPY patches ./patches
RUN pnpm config set registry https://registry.npmmirror.com && \
    pnpm install --frozen-lockfile

FROM deps AS build
# Match the runner OpenSSL so generate ships debian-openssl-3.0.x engines.
RUN apt-get update && apt-get install -y --no-install-recommends openssl ca-certificates && \
    rm -rf /var/lib/apt/lists/*
COPY . .
RUN pnpm exec prisma generate && \
    pnpm build

FROM base AS runner
ENV NODE_ENV=production
WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends openssl ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd -r nodejs && useradd -r -g nodejs nodeuser && \
    mkdir -p /app/prisma/data && \
    chown -R nodeuser:nodejs /app

# Copy the full pnpm tree from build so Prisma CLI, engines, and the
# generated client stay resolvable (pnpm does not hoist .prisma).
COPY --from=build --chown=nodeuser:nodejs /app/node_modules ./node_modules
COPY --from=build --chown=nodeuser:nodejs /app/dist ./dist
COPY --from=build --chown=nodeuser:nodejs /app/prisma ./prisma
COPY --from=build --chown=nodeuser:nodejs /app/package.json ./package.json
COPY --from=build --chown=nodeuser:nodejs /app/.env.example ./.env.example
COPY --from=build --chown=nodeuser:nodejs /app/scripts/start-api.sh ./scripts/start-api.sh

RUN node -e "const p=require('./package.json'); delete p.type; require('fs').writeFileSync('./package.json', JSON.stringify(p, null, 2))" && \
    chmod +x /app/scripts/start-api.sh

USER nodeuser
EXPOSE 3000
CMD ["/app/scripts/start-api.sh"]
