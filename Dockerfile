FROM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
ARG APP_VERSION
ENV APP_VERSION=${APP_VERSION}
RUN npm run build

FROM node:22-bookworm-slim AS languages
WORKDIR /opt/fjord/languages
COPY languages/package.json languages/package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts --engine-strict --no-audit --no-fund

FROM golang:1.27.3-bookworm AS build
WORKDIR /src
ENV GOWORK=off
COPY go.mod go.sum ./
RUN go mod download
COPY scripts/install-language-servers.sh ./scripts/install-language-servers.sh
RUN GOBIN=/out sh scripts/install-language-servers.sh
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG APP_VERSION
ARG VERSION
RUN release_version="${VERSION#v}" \
    && go build -trimpath -ldflags "-X main.version=${release_version:-$APP_VERSION}" -o /out/fjord ./cmd/fjord

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends bubblewrap ca-certificates gh libstdc++6 libatomic1 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home fjord
COPY --from=build /usr/local/go /usr/local/go
COPY --from=build /out/ /opt/fjord/bin/
COPY --from=languages /usr/local/bin/node /opt/fjord/bin/node
COPY --from=languages /opt/fjord/languages /opt/fjord/languages
ENV PATH=/opt/fjord/bin:/usr/local/go/bin:/usr/bin:/bin
USER 10001:10001
WORKDIR /home/fjord
EXPOSE 4727
ENTRYPOINT ["/opt/fjord/bin/fjord"]
CMD ["-addr", "0.0.0.0:4727", "-host", "localhost"]
