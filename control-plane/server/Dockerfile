FROM node:22-alpine AS frontend-build
WORKDIR /src/control-plane/frontend
COPY control-plane/frontend/package.json control-plane/frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --include=dev
COPY control-plane/frontend ./
RUN npm run build

FROM eclipse-temurin:21-jdk AS build
WORKDIR /src/control-plane/server

COPY control-plane/server/gradle ./gradle
COPY control-plane/server/gradlew control-plane/server/gradle.properties control-plane/server/settings.gradle.kts control-plane/server/build.gradle.kts ./
COPY control-plane/contracts ../contracts
COPY control-plane/server/src ./src
COPY --from=frontend-build /src/control-plane/frontend/dist ./src/main/resources/static
RUN --mount=type=cache,target=/root/.gradle chmod +x gradlew && ./gradlew bootJar --no-daemon

FROM eclipse-temurin:21-jre
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 boardproxy \
    && useradd --system --uid 10001 --gid boardproxy boardproxy
WORKDIR /app
COPY --from=build /src/control-plane/server/build/libs/*.jar /app/control-plane.jar
RUN mkdir -p /var/lib/boardproxy-control/pki && chown -R boardproxy:boardproxy /var/lib/boardproxy-control
USER boardproxy
EXPOSE 8080 8443
ENTRYPOINT ["java", "-XX:MaxRAMPercentage=75", "-jar", "/app/control-plane.jar"]
