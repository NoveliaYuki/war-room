# Build the static Go binaries and ship them in a minimal runtime image.
FROM golang:1.25-alpine AS builder

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build -a -installsuffix cgo \
    -ldflags="-s -w -extldflags '-static'" \
    -o /bin/war-room-server \
    ./cmd/server

RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w -extldflags '-static'" \
    -o /bin/healthcheck \
    ./cmd/healthcheck

RUN mkdir -p /empty_root/data/attachments /empty_root/data/logos /empty_root/tmp \
    && chown -R 65532:65532 /empty_root/data /empty_root/tmp

FROM scratch

LABEL maintainer="War Room Team" \
      description="War Room Candidate Selection Process Command Center Backend" \
      version="0.0.1"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

COPY --from=builder /empty_root /

COPY --from=builder /bin/war-room-server /server
COPY --from=builder /bin/healthcheck /healthcheck

ENV PORT=4040 \
    HOST=0.0.0.0 \
    WARROOM_DATA_DIR=/data \
    TMPDIR=/tmp

VOLUME ["/data"]

EXPOSE 4040

USER 65532:65532

HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=4 \
  CMD ["/healthcheck"]

ENTRYPOINT ["/server"]
