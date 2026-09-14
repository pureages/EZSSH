# ---------- 阶段 1：构建前端 ----------
# 固定在构建平台执行：前端产物（静态资源）与 CPU 架构无关。
# 多架构构建时，arm64 也无需用 QEMU 模拟重跑 npm install / vite build。
FROM --platform=$BUILDPLATFORM node:20-alpine AS web-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# ---------- 阶段 2：构建后端 ----------
# 同样固定在构建平台，用 GOARCH=$TARGETARCH 交叉编译出目标架构的纯静态二进制
# （CGO_ENABLED=0，无需目标架构的 C 工具链，因此不必依赖 QEMU 模拟编译）。
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS go-builder
ARG TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
ENV GOPROXY=https://proxy.golang.org,direct
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux GOARCH="$TARGETARCH" go build -buildvcs=false -ldflags="-s -w" -o /ezssh ./cmd/ezssh

# ---------- 阶段 3：运行 ----------
# 本阶段按目标架构执行（apk/user 需要目标架构），但耗时极短，可忽略 QEMU 开销
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
