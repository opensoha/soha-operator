FROM --platform=$BUILDPLATFORM golang:1.26.6-alpine AS build
ARG TARGETOS=linux
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN if [ -z "$TARGETARCH" ]; then TARGETARCH="$(go env GOARCH)"; fi; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/soha-operator ./cmd/operator

FROM alpine:3.20
RUN addgroup -S -g 10001 soha && adduser -S -D -H -u 10001 -G soha soha && apk add --no-cache ca-certificates
COPY --from=build /out/soha-operator /soha-operator
USER 10001:10001
EXPOSE 8080 8081
ENTRYPOINT ["/soha-operator"]
