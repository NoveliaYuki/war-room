FROM golang:1.25-bookworm AS go-runtime
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download

FROM golangci/golangci-lint:v2.13.2 AS golangci

FROM mcr.microsoft.com/playwright:v1.61.1-noble
ARG BASE_VERSION
ARG VERSION_BUMP=patch
COPY --from=go-runtime /usr/local/go /usr/local/go
COPY --from=go-runtime /go/pkg/mod /go/pkg/mod
COPY --from=golangci /usr/bin/golangci-lint /usr/bin/golangci-lint
ENV PATH="/usr/local/go/bin:${PATH}" GOCACHE=/tmp/go-cache CI=true BASE_VERSION=${BASE_VERSION} VERSION_BUMP=${VERSION_BUMP}

WORKDIR /workspace
COPY package.json ./
COPY VERSION README.md ./
COPY frontend/package.json frontend/package-lock.json ./frontend/
COPY e2e/go.mod ./e2e/
RUN npm ci --prefix frontend && go -C e2e mod download
RUN go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6100.0 install chromium

COPY backend ./backend
COPY frontend ./frontend
COPY e2e ./e2e
COPY scripts ./scripts
COPY go.work ./

CMD ["npm", "test"]
