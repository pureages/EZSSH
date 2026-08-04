# ---------- 阶段 1：构建前端 ----------
FROM node:20-alpine AS web-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# ---------- 阶段 2：构建后端 ----------
FROM golang:1.25-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
ENV GOPROXY=https://proxy.golang.org,direct
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -ldflags="-s -w" -o /ezssh ./cmd/ezssh

# ---------- 阶段 3：运行 ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && addgroup -S ezssh && adduser -S ezssh -G ezssh
WORKDIR /app
COPY --from=go-builder /ezssh /app/ezssh
COPY --from=web-builder /app/web/dist /app/web/dist
RUN mkdir -p /data /app/data && chown -R ezssh:ezssh /app /data
USER ezssh
ENV EZSSH_LISTEN=0.0.0.0 \
    EZSSH_PORT=49466 \
    EZSSH_DATA=/app/data
VOLUME ["/app/data"]
EXPOSE 49466
CMD ["/app/ezssh"]
