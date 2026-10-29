FROM golang:1.23-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
ARG TARGETARCH
ARG SOURCE_COMMIT=dev
WORKDIR /src
COPY go.mod ./
# Include all root-level Go sources, including newly added API modules.
COPY *.go ./
COPY internal ./internal
COPY web ./web
# CGO + SQLite: build runs under the target architecture via buildx/QEMU for multiarch.
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w -X main.buildCommit=${SOURCE_COMMIT}" -o /out/familiehub .

FROM alpine:3.21
RUN apk add --no-cache sqlite-libs ca-certificates tzdata \
 && addgroup -S -g 10001 familiehub \
 && adduser -S -D -H -u 10001 -G familiehub familiehub \
 && mkdir -p /data \
 && chown 10001:10001 /data
WORKDIR /app
COPY --from=builder /out/familiehub /app/familiehub
ENV DATA_DIR=/data TZ=Europe/Copenhagen
EXPOSE 8080
USER 10001:10001
ENTRYPOINT ["/app/familiehub"]
CMD ["-listen","0.0.0.0:8080"]
