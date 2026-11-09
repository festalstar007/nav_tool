FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.10-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' -o /navtool ./cmd/navtool

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -g 10001 navtool && adduser -D -u 10001 -G navtool navtool && mkdir /data && chown navtool:navtool /data
WORKDIR /app
COPY --from=backend /navtool /app/navtool
COPY --from=frontend /web/dist /app/web/dist
USER 10001:10001
ENV NAV_ADDR=0.0.0.0:31506 NAV_DATA_DIR=/data NAV_ORIGIN=https://nav.345001.xyz
EXPOSE 31506
ENTRYPOINT ["/app/navtool"]
