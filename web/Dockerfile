# ---- build stage ----
FROM node:24-alpine AS build

WORKDIR /app

# `npm ci` needs both files and installs exactly the locked tree.
COPY package.json package-lock.json* ./
RUN --mount=type=cache,target=/root/.npm \
    if [ -f package-lock.json ]; then npm ci; else npm install; fi

COPY . .

# The Capacitor native projects are large and irrelevant to a web image.
RUN rm -rf android ios

# The same origin serves the API in production, so no API base is baked in
# and the app talks to whichever host it was loaded from.
ENV VITE_API_BASE=""

RUN npm run build

# ---- runtime stage ----
FROM nginx:1.27-alpine

# The compiled app and nothing else.
COPY --from=build /app/dist /usr/share/nginx/html

COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1/healthz >/dev/null || exit 1

CMD ["nginx", "-g", "daemon off;"]
