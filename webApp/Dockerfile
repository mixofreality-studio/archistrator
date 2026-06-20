FROM node:20-alpine AS builder
WORKDIR /app

# Runtime SPA config is baked at build time (config.ts reads import.meta.env.VITE_*).
# The archistrator-release workflow passes these as --build-arg; Vite picks them up
# from the environment during `vite build`. Defaults keep local `docker build`
# working without args (dev auth mode).
ARG VITE_API_BASE_URL=""
ARG VITE_AUTH_MODE="dev"
ARG VITE_OIDC_AUTHORITY=""
ARG VITE_OIDC_CLIENT_ID="archistrator-webapp"
ARG VITE_OIDC_REDIRECT_URI=""
ARG VITE_OIDC_SCOPE="openid profile email"
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL \
    VITE_AUTH_MODE=$VITE_AUTH_MODE \
    VITE_OIDC_AUTHORITY=$VITE_OIDC_AUTHORITY \
    VITE_OIDC_CLIENT_ID=$VITE_OIDC_CLIENT_ID \
    VITE_OIDC_REDIRECT_URI=$VITE_OIDC_REDIRECT_URI \
    VITE_OIDC_SCOPE=$VITE_OIDC_SCOPE

COPY package.json package-lock.json ./
RUN npm ci
COPY . .
# The generated API client (src/api/schema.ts) is committed, so the build does
# not need the server's openapi.yaml (which lives outside this build context).
# Skip the prebuild gen:api step; regeneration is a dev-time `npm run gen:api`.
RUN npx tsc -b && npx vite build

FROM nginx:alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
