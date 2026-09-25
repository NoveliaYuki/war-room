# Validate assets in the build stage; serve them from the production image.
FROM node:24-alpine AS assets

WORKDIR /src
COPY package.json ./package.json
COPY public ./public
COPY demo ./demo
COPY index.html ./index.html
RUN find public/js demo -type f -name '*.js' -exec node --check {} +

FROM nginx:alpine-slim AS runtime

LABEL maintainer="War Room Team" \
      description="War Room Candidate Selection Process Command Center Frontend" \
      version="0.0.3"

RUN rm -rf /etc/nginx/conf.d/default.conf /usr/share/nginx/html/*

COPY nginx.conf /etc/nginx/conf.d/war-room.conf
COPY security-headers.conf /etc/nginx/war-room-security-headers.conf

COPY --from=assets /src/index.html /usr/share/nginx/html/index.html
COPY --from=assets /src/public /usr/share/nginx/html/public
COPY --from=assets /src/demo /usr/share/nginx/html/demo

EXPOSE 80

HEALTHCHECK --interval=5s --timeout=2s --start-period=3s --retries=5 \
  CMD wget -qO- http://127.0.0.1/ > /dev/null || exit 1

CMD ["nginx", "-g", "daemon off;"]
