# Stage 1: Build
FROM golang:1.25.1-alpine AS builder 

WORKDIR /app
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app-binary ./server/main.go

FROM gcr.io/distroless/base-debian12
WORKDIR /

COPY --from=builder /app-binary .

EXPOSE 8084
USER nonroot:nonroot

ENTRYPOINT [ "./app-binary" ]