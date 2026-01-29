FROM node:22-alpine

WORKDIR /app
COPY discogs-listen-tracker/frontend/package.json frontend/package.json
COPY discogs-listen-tracker/frontend/pnpm-lock.yaml frontend/pnpm-lock.yaml

RUN corepack enable && corepack prepare pnpm@10.28.0 --activate

WORKDIR /app/frontend
RUN pnpm install

COPY discogs-listen-tracker/frontend/ /app/frontend/

EXPOSE 5173
CMD ["pnpm", "dev", "--host", "0.0.0.0", "--port", "5173"]

