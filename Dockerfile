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
ARG WITH_TGS=false
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && if [ "$WITH_FFMPEG" = true ] || [ "$WITH_TGS" = true ]; then apt-get install -y --no-install-recommends ffmpeg; fi && rm -rf /var/lib/apt/lists/*
# Optional on-demand TGS renderer; leave the lightweight base image unchanged.
RUN if [ "$WITH_TGS" = true ]; then apt-get update && apt-get install -y --no-install-recommends python3 python3-venv && python3 -m venv /opt/tgs && /opt/tgs/bin/pip install --no-cache-dir rlottie-python==1.3.8 Pillow==12.3.0 && rm -rf /var/lib/apt/lists/*; fi
ENV PATH="/opt/tgs/bin:${PATH}"
COPY --from=build /out/laowangbot /usr/local/bin/laowangbot
WORKDIR /data
VOLUME /data
ENTRYPOINT ["laowangbot"]
CMD ["--supervise", "--root", "/data"]
