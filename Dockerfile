# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/laowangbot ./cmd/laowangbot
FROM debian:bookworm-slim
ARG WITH_FFMPEG=true
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && if [ "$WITH_FFMPEG" = true ]; then apt-get install -y --no-install-recommends ffmpeg; fi && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/laowangbot /usr/local/bin/laowangbot
WORKDIR /data
VOLUME /data
ENTRYPOINT ["laowangbot"]
CMD ["--supervise", "--root", "/data"]
