FROM --platform=$TARGETPLATFORM golang:1.26-alpine AS builder
LABEL maintainer="nekohasekai <contact-git@sekai.icu>"
COPY . /go/src/github.com/sagernet/sing-box
WORKDIR /go/src/github.com/sagernet/sing-box
ARG TARGETOS TARGETARCH TARGETVARIANT
ARG GOPROXY=""
ENV GOPROXY ${GOPROXY}
RUN set -ex \
    && apk add --no-cache git build-base bash \
    && export VERSION=$(go run ./cmd/internal/read_tag) \
    && export BUILD_TAGS=$(cat release/DEFAULT_BUILD_TAGS_OTHERS) \
    && if [ "$TARGETARCH" = arm ]; then export GOARM="${TARGETVARIANT#v}"; fi \
    && GOOS="$TARGETOS" GOARCH="$TARGETARCH" CC=gcc LWIP_MODE=source LWIP_LIBC=musl \
        OUTPUT_DIR=/out bash release/build-native.sh
FROM --platform=$TARGETPLATFORM alpine AS dist
LABEL maintainer="nekohasekai <contact-git@sekai.icu>"
RUN set -ex \
    && apk add --no-cache --upgrade bash tzdata ca-certificates nftables
COPY --from=builder /out/sing-box /usr/local/bin/sing-box
COPY --from=builder /out/licenses /usr/share/licenses/sing-box
ENTRYPOINT ["sing-box"]
