# Build stage
FROM --platform=$BUILDPLATFORM node:26-alpine AS build

WORKDIR /app

COPY services/web-ui/package*.json ./
RUN npm ci --prefer-offline

COPY services/web-ui/. .
RUN npm run build

# Production stage — rootless nginx
FROM nginx:1.31-alpine

# Pick up OS security patches published since the base tag was last rebuilt.
#
# A base tag is a moving pointer, and it moves on the registry's schedule, not
# on the security one: the nightly scan found openssl 3.5.7-r0 (CVE-2026-14456,
# HIGH) in the Alpine bases while Alpine itself had already published 3.5.8-r0.
# Without this line the image we ship carries whatever the base happened to hold
# on the day it was built, and the only remedy is waiting for somebody else to
# rebuild a tag we do not control.
#
# --no-cache leaves no index behind, so the layer does not grow the image.
RUN apk upgrade --no-cache

COPY --from=build /app/dist /usr/share/nginx/html
COPY services/web-ui/nginx.conf /etc/nginx/conf.d/default.conf

# Allow nginx to run as non-root (port 80 via setcap in base image)
RUN chown -R nginx:nginx /usr/share/nginx/html /var/cache/nginx /var/log/nginx && \
    touch /var/run/nginx.pid && chown nginx:nginx /var/run/nginx.pid

USER nginx

EXPOSE 80

# 127.0.0.1, not localhost.
#
# This container runs as USER nginx, so the base image's
# 10-listen-on-ipv6-by-default.sh cannot rewrite the config it does not own —
# it says so at every start ("can not modify /etc/nginx/conf.d/default.conf")
# and nginx ends up listening on 0.0.0.0:80 only. `localhost` resolves to both
# 127.0.0.1 and ::1, and RFC 6724 ranks ::1 first, which is where busybox wget
# went: connection refused, every time, for the life of this image.
#
# nginx served fine throughout — the published port worked, the acceptance
# suite passed through it — so the only visible symptom was the word
# "(unhealthy)" in `docker ps`. It cost the whole site on any deployment with a
# proxy in front: Traefik skips unhealthy containers, so the cloud stack
# answered 404 to everything, and `make onprem-tls` never started Caddy, which
# waits for this container to be healthy.
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:80/nginx-health || exit 1

CMD ["nginx", "-g", "daemon off;"]
