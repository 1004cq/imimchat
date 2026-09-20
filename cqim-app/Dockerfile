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
COPY . .
RUN pnpm exec prisma generate && \
    pnpm build

FROM base AS prod-deps
COPY package.json pnpm-lock.yaml ./
COPY patches ./patches
RUN pnpm config set registry https://registry.npmmirror.com && \
    pnpm install --prod --frozen-lockfile

FROM base AS runner
ENV NODE_ENV=production
WORKDIR /app

RUN groupadd -r nodejs && useradd -r -g nodejs nodeuser && \
    mkdir -p /app/prisma/data && \
    chown -R nodeuser:nodejs /app

COPY --from=prod-deps --chown=nodeuser:nodejs /app/node_modules ./node_modules
# Keep the Prisma CLI and engines available for the migration entrypoint.
COPY --from=deps --chown=nodeuser:nodejs /app/node_modules/prisma ./node_modules/prisma
COPY --from=deps --chown=nodeuser:nodejs /app/node_modules/@prisma ./node_modules/@prisma
COPY --from=build --chown=nodeuser:nodejs /app/dist ./dist
COPY --from=build --chown=nodeuser:nodejs /app/prisma ./prisma
COPY --from=build --chown=nodeuser:nodejs /app/package.json ./package.json
COPY --from=build --chown=nodeuser:nodejs /app/.env.example ./.env.example
COPY --from=build --chown=nodeuser:nodejs /app/scripts/start-api.sh ./scripts/start-api.sh

# pnpm does not hoist node_modules/.prisma; generate with the copied CLI.
# Do not use `pnpm exec prisma` here: prisma is a devDependency and is not on PATH.
RUN node -e "const p=require('./package.json'); delete p.type; require('fs').writeFileSync('./package.json', JSON.stringify(p, null, 2))" && \
    chmod +x /app/scripts/start-api.sh && \
    DATABASE_URL="postgresql://cqim:cqim@postgres:5432/cqim?schema=public" \
      node /app/node_modules/prisma/build/index.js generate && \
    chown -R nodeuser:nodejs /app

USER nodeuser
EXPOSE 3000
CMD ["/app/scripts/start-api.sh"]
