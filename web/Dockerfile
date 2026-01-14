# CSCAN Web 前端 Dockerfile
# 支持多架构: linux/amd64, linux/arm64

FROM node:24-alpine AS builder

WORKDIR /app

COPY package*.json ./
RUN npm install

COPY . .

# 添加构建时间戳，强制每次都重新构建
ARG BUILD_TIME
ENV BUILD_TIME=$BUILD_TIME

RUN npm run build

FROM nginx:alpine

# 安装 openssl 用于生成自签证书
RUN apk add --no-cache openssl

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

EXPOSE 80 443

ENTRYPOINT ["/docker-entrypoint.sh"]
