# Build context is the repo root: the frontend type-checks and bundles
# against ../openapi/schema.ts (the @openapi alias).
FROM node:alpine AS build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
# --ignore-scripts closes the postinstall supply-chain vector; nothing in the
# tree needs lifecycle scripts (only darwin-only fsevents has one).
RUN npm ci --ignore-scripts
COPY openapi/schema.ts /src/openapi/schema.ts
COPY frontend/ ./
# Browser-visible API origin, baked into the bundle. Overrides .env.production.
ARG VITE_API_URL
ENV VITE_API_URL=$VITE_API_URL
RUN npm run build

FROM nginx:alpine
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/frontend/dist /usr/share/nginx/html
