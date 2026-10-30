FROM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
ARG APP_VERSION
ENV APP_VERSION=${APP_VERSION}
RUN npm run build

FROM golang:1.27.3-bookworm AS build
WORKDIR /src
ENV GOWORK=off
COPY go.mod go.sum ./
RUN go mod download
RUN GOBIN=/out go install golang.org/x/tools/gopls@v0.24.0 \
    && GOBIN=/out go install github.com/opentofu/tofu-ls@v0.5.3
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG APP_VERSION
RUN go build -trimpath -ldflags "-X main.version=${APP_VERSION}" -o /out/fjord ./cmd/fjord

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends bubblewrap ca-certificates gh \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home fjord
COPY --from=build /usr/local/go /usr/local/go
COPY --from=build /out/ /opt/fjord/bin/
ENV PATH=/opt/fjord/bin:/usr/local/go/bin:/usr/bin:/bin
USER 10001:10001
WORKDIR /home/fjord
EXPOSE 4727
ENTRYPOINT ["/opt/fjord/bin/fjord"]
CMD ["-addr", "0.0.0.0:4727", "-host", "localhost"]
