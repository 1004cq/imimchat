FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/go-api ./cmd/go-api

FROM alpine:3.20
RUN addgroup -S cqim && adduser -S -G cqim cqim
USER cqim
COPY --from=build /out/go-api /usr/local/bin/go-api
EXPOSE 8089
ENTRYPOINT ["go-api"]
