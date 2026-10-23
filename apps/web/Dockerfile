# 빌드 컨텍스트는 저장소 루트다. 루트 package-lock.json으로 재현 가능한 npm ci를 수행한다.
# 베이스 이미지는 멀티아키텍처 index digest로 고정한다(linux/amd64, linux/arm64).
# 갱신 절차는 docs/installation/docker-images.md를 따른다.
ARG NODE_IMAGE=node:24.22.1-alpine@sha256:06d7adf3e0ef61a13f1d28682726366f97a15aa1888c934d3ecec86c33918b53
ARG NGINX_IMAGE=nginxinc/nginx-unprivileged:1.30.6-alpine@sha256:f5116ed4606f3eb015dd4a8e28cb9b351476dc0ad4bc6e9f93b19cec130cdf03

FROM ${NODE_IMAGE} AS build

ENV npm_config_fund=false \
    npm_config_update_notifier=false

WORKDIR /app
COPY package.json package-lock.json ./
COPY apps/web/package.json ./apps/web/
COPY packages/event-schema/package.json ./packages/event-schema/
RUN npm ci --workspace @agent-office/web --include-workspace-root=false
COPY apps/web ./apps/web
RUN npm --workspace @agent-office/web run build

FROM ${NGINX_IMAGE} AS runtime

ARG VERSION=0.0.0-dev
ARG REVISION=unknown

LABEL org.opencontainers.image.title="agent-office-web" \
      org.opencontainers.image.description="Agent Office web UI" \
      org.opencontainers.image.source="https://github.com/apg0001/Agent-Office" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

COPY --from=build /app/apps/web/dist /usr/share/nginx/html

# nginx-unprivileged 이미지는 UID 101로 8080 포트에서 실행한다.
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/"]
