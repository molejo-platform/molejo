# syntax=docker/dockerfile:1.20

FROM docker.io/library/node:24.19.0-alpine@sha256:d32cdf619f63fe0471182d08996dd516c6275bb5fd31ae06e55a570bd9e1ad43 AS build

ARG VERSION=devel
ARG COMMIT=unknown
ENV VITE_APP_VERSION=${VERSION}
ENV VITE_APP_COMMIT=${COMMIT}

RUN corepack enable \
    && rm -rf /usr/local/lib/node_modules/npm /usr/local/bin/npm /usr/local/bin/npx
WORKDIR /workspace
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/console-web/package.json apps/console-web/package.json
RUN pnpm install --frozen-lockfile --ignore-scripts --filter @molejo-platform/console-web
COPY apps/console-web/ apps/console-web/
RUN pnpm --filter @molejo-platform/console-web build

FROM docker.io/library/nginx:1.30.4-alpine-slim@sha256:ddde39c6e51f02fde7410c2e9c234cf2d0a4c7bdbbe176aeb37d8ad7ab4eb58c

COPY apps/console-web/nginx.conf /etc/nginx/nginx.conf
COPY apps/console-web/default.conf /etc/nginx/conf.d/default.conf
RUN find /usr/share/nginx/html -mindepth 1 -delete
COPY --from=build --chown=65532:65532 /workspace/apps/console-web/dist/ /usr/share/nginx/html/

USER 65532:65532
EXPOSE 8080
STOPSIGNAL SIGQUIT
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]
