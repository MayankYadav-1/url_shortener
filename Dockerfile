FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /url-shortener .

FROM alpine:3.22
RUN adduser -D -H appuser
USER appuser
COPY --from=build /url-shortener /url-shortener
EXPOSE 8080
ENTRYPOINT ["/url-shortener"]
