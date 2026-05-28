FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/quote-service ./cmd/quote-service

FROM alpine:3.22

RUN adduser -D -H quote
USER quote
WORKDIR /app
COPY --from=build /out/quote-service /app/quote-service
COPY config.yaml /app/config.yaml
COPY config.docker.yaml /app/config.docker.yaml

EXPOSE 8080
ENV CONFIG_PATH=/app/config.yaml
ENV EXCHANGERATE_KEY=***
ENTRYPOINT ["/app/quote-service"]
