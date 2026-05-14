# syntax=docker/dockerfile:1.7

FROM docker.io/library/golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/domux ./cmd/domux && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/domux-agent ./cmd/domux-agent

FROM docker.io/library/alpine:3.22 AS runtime-base
RUN apk add --no-cache ca-certificates openssh-client tzdata
WORKDIR /var/lib/domux

FROM runtime-base AS domux
COPY --from=build /out/domux /usr/local/bin/domux
EXPOSE 18080 8080 8443
ENTRYPOINT ["/usr/local/bin/domux"]
CMD ["-config", "/etc/domux/config.yaml"]

FROM runtime-base AS domux-agent
COPY --from=build /out/domux-agent /usr/local/bin/domux-agent
EXPOSE 8890
ENTRYPOINT ["/usr/local/bin/domux-agent"]
CMD ["-listen", ":8890"]
