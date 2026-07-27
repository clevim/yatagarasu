# --platform=$BUILDPLATFORM: the build stage runs natively on the runner and
# cross-compiles via TARGETARCH below. Without it buildx emulates the whole Go
# toolchain under QEMU for the arm64 image, turning 20 seconds into minutes.
#
# The default is what keeps `docker build` working without buildx: the legacy
# builder never sets BUILDPLATFORM, and an empty --platform is a parse error.
ARG BUILDPLATFORM=linux/amd64
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY yata.koplugin ./yata.koplugin
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags "-s -w" -o /yata .

FROM scratch
COPY --from=build /yata /yata
VOLUME /data
EXPOSE 3080
ENTRYPOINT ["/yata"]
