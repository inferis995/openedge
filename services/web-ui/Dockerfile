# Build stage
FROM node:20-alpine as build

WORKDIR /app

COPY services/web-ui/package*.json ./
RUN npm install

COPY services/web-ui/. .
RUN npm run build

# Production stage
FROM nginx:alpine

COPY --from=build /app/dist /usr/share/nginx/html

# Custom nginx config to handle client-side routing and API proxy
# Copy custom nginx config
COPY services/web-ui/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
