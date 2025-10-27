# syntax=docker/dockerfile:1.5

#===============
# Stage 1: Build
#===============
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache fuse-dev gcc g++ musl-dev make git

WORKDIR /app

# Cache go modules first
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the rest of the source
COPY . .

# Build with cache mounts
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    BIN_OUTPUT=/bin/distribyted make build

#===============
# Stage 2: Run
#===============
FROM alpine:3.20

# Runtime dependencies
RUN apk add --no-cache fuse libstdc++ libgcc su-exec

COPY --from=builder /bin/distribyted /bin/distribyted
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /bin/distribyted /entrypoint.sh \
    && mkdir -p /data /config \
    && chmod 0777 /data /config

# FUSE allow_other
RUN echo "user_allow_other" >> /etc/fuse.conf
ENV DISTRIBYTED_FUSE_ALLOW_OTHER=true
ENV DISTRIBYTED_CONFIG=/config/config.yaml

ENTRYPOINT [ "/entrypoint.sh" ]
CMD [ "/bin/distribyted" ]
