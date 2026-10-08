FROM golang:1.26-bookworm AS go-runtime
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download

FROM golangci/golangci-lint:v2.13.2 AS golangci

FROM mcr.microsoft.com/playwright:v1.63.0-noble
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get upgrade --yes \
    && apt-get remove --yes libass9 libsrt1.5-gnutls \
    && rm -rf /var/lib/apt/lists/*
COPY --from=go-runtime /usr/local/go /usr/local/go
COPY --from=go-runtime /go/pkg/mod /go/pkg/mod
COPY --from=golangci /usr/bin/golangci-lint /usr/bin/golangci-lint
ENV PATH="/usr/local/go/bin:${PATH}" GOCACHE=/tmp/go-cache CI=true

WORKDIR /workspace
COPY package.json ./
COPY VERSION README.md ./
COPY frontend/package.json frontend/package-lock.json ./frontend/
COPY e2e/go.mod ./e2e/
RUN npm ci --prefix frontend && go -C e2e mod download
RUN go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6201.1 install chromium

COPY backend ./backend
COPY frontend ./frontend
COPY e2e ./e2e
COPY scripts ./scripts
COPY go.work ./

CMD ["npm", "test"]
