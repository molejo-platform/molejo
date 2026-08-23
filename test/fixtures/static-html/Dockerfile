# syntax=docker/dockerfile:1.20

FROM scratch AS content
COPY test/fixtures/static-html/index.html /site/index.html
COPY test/fixtures/static-html/about.html /site/about.html
COPY test/fixtures/static-html/assets/ /site/assets/

FROM docker.io/library/nginx:1.30.4-alpine-slim@sha256:ddde39c6e51f02fde7410c2e9c234cf2d0a4c7bdbbe176aeb37d8ad7ab4eb58c

COPY test/fixtures/frontend-runtime/nginx.conf /etc/nginx/nginx.conf
COPY test/fixtures/static-html/default.conf /etc/nginx/conf.d/default.conf
RUN find /usr/share/nginx/html -mindepth 1 -delete
COPY --from=content --chown=65532:65532 /site/ /usr/share/nginx/html/

USER 65532:65532
EXPOSE 8080
STOPSIGNAL SIGQUIT
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]
