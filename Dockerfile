# syntax=docker/dockerfile:1

# Multistage Dockerfile for Go builds
# NOTE: More info can be found here: https://docs.docker.com/build/guide/multi-stage/
# NOTE: See https://github.com/reproducible-containers/buildkit-cache-dance

ARG GO_VERSION=1.23.1

# Base container to cache go.mod dependencies
FROM golang:${GO_VERSION} AS base

ARG CGO_ENABLED=1
ARG TARGET_PLATFORM=linux
ARG TARGET_ARCH=amd64
ARG ARCH=${TARGET_ARCH}

RUN rm -f /etc/apt/apt.conf.d/docker-clean; echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache

# NOTE: we don't care about pinning versions for root certs
# hadolint ignore=DL3008
RUN \
  --mount=type=cache,target=/var/cache/apt,sharing=locked \
  --mount=type=cache,target=/var/lib/apt,sharing=locked \
  apt-get update && apt-get -y upgrade && \
  apt-get install --no-install-recommends -y ca-certificates

WORKDIR /build

# Download Go modules
COPY go.mod ./
COPY go.sum ./

RUN \
  # NOTE: Set the command to use the attached cache
  --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,target=/root/go/pkg/mod \
  go mod download -x

# Copy source
COPY . .

###################################################################################################
# Build stage
###################################################################################################

# NOTE: CGO creates dynamic links to various bits of libc so we need to ensure they get copied over to the scratch image
# We do this by first listing the dynamic links with ldd and moving them to the output directory ready for copy
# See https://blog.2read.net/posts/building-a-minimalist-docker-container-with-alpine-linux-and-golang/ for more info

# hadolint ignore=DL4006
RUN \
  --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,target=/root/go/pkg/mod \
  go env -w CGO_ENABLED=${CGO_ENABLED}  &&\
  go env -w GOOS=${TARGET_PLATFORM} && \
  go env -w GOARCH=${TARGET_ARCH} && \
  go build -trimpath -o /dist/agent . &&\
  ldd /dist/agent | tr -s '[:blank:]' '\n' | grep ^/ | xargs -I % install -D % /dist/%

FROM scratch AS agent

COPY --from=base --chmod=755 /dist /
ENTRYPOINT ["/agent"]
