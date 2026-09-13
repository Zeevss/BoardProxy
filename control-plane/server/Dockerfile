# Обе стадии сборки прибиты к платформе сборщика и всегда идут нативно.
#
# Собирать их под целевую архитектуру незачем: npm выдаёт JS и CSS, Gradle —
# байт-код JVM, и то и другое от архитектуры не зависит. Под arm64 меняется
# только рантайм-образ. Без этих пинов buildx прогнал бы весь npm и Gradle под
# QEMU — десятки минут ради побайтово того же результата.
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-build
WORKDIR /src/control-plane/frontend
COPY control-plane/frontend/package.json control-plane/frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --include=dev
COPY control-plane/frontend ./
RUN npm run build

FROM --platform=$BUILDPLATFORM eclipse-temurin:21-jdk AS build
# Кэш Gradle живёт слоем, а не cache mount'ом: BuildKit не выгружает mount'ы
# через `type=gha`, поэтому в CI такой кэш каждый раз пустой, и любая правка
# исходника тянула бы повторную выкачку всех зависимостей Spring и gRPC.
ENV GRADLE_USER_HOME=/gradle
WORKDIR /src/control-plane/server

COPY control-plane/server/gradle ./gradle
COPY control-plane/server/gradlew control-plane/server/gradle.properties control-plane/server/settings.gradle.kts control-plane/server/build.gradle.kts ./
# Отдельным слоем кладём дистрибутив Gradle: он не меняется от правок кода.
RUN chmod +x gradlew && ./gradlew --no-daemon --version
COPY control-plane/contracts ../contracts
COPY control-plane/server/src ./src
COPY --from=frontend-build /src/control-plane/frontend/dist ./src/main/resources/static
ARG VERSION=0.1.0-SNAPSHOT
RUN ./gradlew bootJar --no-daemon -PappVersion="$VERSION"

FROM eclipse-temurin:21-jre
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 boardproxy \
    && useradd --system --uid 10001 --gid boardproxy boardproxy
WORKDIR /app
# Шаблон обязан совпасть ровно с одним файлом — назначение здесь файл, а не
# каталог. Держится это на том, что выше запускается только `bootJar`: добавьте
# в ту же команду `test` или `build`, и рядом ляжет `-plain.jar`, а сборка
# упадёт на «must be a directory».
COPY --from=build /src/control-plane/server/build/libs/*.jar /app/control-plane.jar
RUN mkdir -p /var/lib/boardproxy-control/pki && chown -R boardproxy:boardproxy /var/lib/boardproxy-control
USER boardproxy
EXPOSE 8080 8443
ENTRYPOINT ["java", "-XX:MaxRAMPercentage=75", "-jar", "/app/control-plane.jar"]
