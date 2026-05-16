FROM golang:1.26.1-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/zhenis-orda ./cmd

FROM alpine:3.22

WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/zhenis-orda /app/zhenis-orda
COPY static /app/static
RUN mkdir -p /app/data /app/uploads/books /app/uploads/free-lessons

EXPOSE 8080
CMD ["/app/zhenis-orda"]
