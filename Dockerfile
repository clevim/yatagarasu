FROM golang:1.26-alpine AS build
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
EXPOSE 8080
ENTRYPOINT ["/yata"]
