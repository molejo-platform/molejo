# syntax=docker/dockerfile:1.20

FROM docker.io/library/node:24.19.0-alpine@sha256:d32cdf619f63fe0471182d08996dd516c6275bb5fd31ae06e55a570bd9e1ad43 AS build

RUN corepack enable \
    && rm -rf /usr/local/lib/node_modules/npm /usr/local/bin/npm /usr/local/bin/npx
WORKDIR /workspace
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY test/fixtures/vite-react-spa/package.json test/fixtures/vite-react-spa/package.json
RUN pnpm install --frozen-lockfile --filter @fruto-platform/vite-react-spa

COPY test/fixtures/vite-react-spa/ test/fixtures/vite-react-spa/
ARG APP_VERSION=dev
ENV VITE_APP_VERSION=${APP_VERSION}
RUN pnpm --filter @fruto-platform/vite-react-spa build

FROM docker.io/library/nginx:1.30.4-alpine-slim@sha256:ddde39c6e51f02fde7410c2e9c234cf2d0a4c7bdbbe176aeb37d8ad7ab4eb58c

COPY test/fixtures/frontend-runtime/nginx.conf /etc/nginx/nginx.conf
COPY test/fixtures/vite-react-spa/default.conf /etc/nginx/conf.d/default.conf
RUN find /usr/share/nginx/html -mindepth 1 -delete
COPY --from=build --chown=65532:65532 /workspace/test/fixtures/vite-react-spa/dist/ /usr/share/nginx/html/

USER 65532:65532
EXPOSE 8080
STOPSIGNAL SIGQUIT
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]
