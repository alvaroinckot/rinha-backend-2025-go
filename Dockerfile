FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app .


#### PROD


FROM scratch
COPY --from=builder /out/app /app
EXPOSE 9999
ENTRYPOINT ["/app"]


#### PROFILE

# FROM debian:bookworm-slim AS profiler
# RUN apt-get update && \
#     apt-get install -y --no-install-recommends \
#       linux-perf \
#       procps       \   
#       curl          \  
#       ca-certificates \
#       graphviz       \
#     && rm -rf /var/lib/apt/lists/*

# # Copy tiny static binary
# COPY --from=builder /out/app /app

# # HTTP port for your service + pprof listener (see note below)
# EXPOSE 9999 6060

# # Optional: small GC tweak, helps keep heap high-water mark down
# ENV GODEBUG=madvdontneed=1

# ENTRYPOINT ["/app"]