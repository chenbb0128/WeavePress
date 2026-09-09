FROM golang@sha256:e401dae1bf814e29204a8cb7915682e1780951e609ca0dd8865ee1937f510c48 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGET=api
ARG APP_SHA
RUN printf '%s' "$APP_SHA" | grep -Eq '^[0-9a-f]{40}$'
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${TARGET}
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian12:nonroot
ARG APP_SHA
LABEL org.opencontainers.image.revision=$APP_SHA
WORKDIR /app
COPY --from=build /out/app /app/app
COPY --from=build /out/healthcheck /app/healthcheck
COPY configs /app/configs
COPY database/migrations /app/database/migrations
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/app"]
