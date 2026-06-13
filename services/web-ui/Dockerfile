# Build stage
FROM --platform=$BUILDPLATFORM node:20-alpine AS build

WORKDIR /app

COPY services/web-ui/package*.json ./
RUN npm ci --prefer-offline

COPY services/web-ui/. .
RUN npm run build

# Production stage — rootless nginx
FROM nginx:1.27-alpine

COPY --from=build /app/dist /usr/share/nginx/html
COPY services/web-ui/nginx.conf /etc/nginx/conf.d/default.conf

# Allow nginx to run as non-root (port 80 via setcap in base image)
RUN chown -R nginx:nginx /usr/share/nginx/html /var/cache/nginx /var/log/nginx && \
    touch /var/run/nginx.pid && chown nginx:nginx /var/run/nginx.pid

USER nginx

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:80/nginx-health || exit 1

CMD ["nginx", "-g", "daemon off;"]
