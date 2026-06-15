# syntax=docker/dockerfile:1

# 1. Build the portal micro-frontend (Vite + TS → portal/dist) in a node
#    stage. portal/ is a self-contained npm project so we only need its
#    package.json/lockfile + source — no host-side npm install required.
FROM node:22-alpine AS portal
WORKDIR /portal
COPY portal/package.json portal/package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY portal/ ./
RUN npm run build

# 2. Build the Go binary. assets.go embeds portal/dist via //go:embed, so
#    the dist/ output from the previous stage has to land at the right
#    relative path before `go build` runs.
#
# TODO(sdk-publish): depends on github.com/faroshq/kedge-provider-sdk via a
# `replace => ../../provider-sdk` that only resolves in the monorepo (go.work).
# Standalone image builds need the SDK published (drop the replace) or vendored.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go assets.go init_cmd.go ./
COPY --from=portal /portal/dist ./portal/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/quickstart-provider .

# 3. Minimal runtime image. Portal assets are baked into the binary; the
#    APIResourceSchemas the `init` subcommand applies are baked at
#    /etc/kedge/schemas (KEDGE_SCHEMAS_DIR).
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/quickstart-provider /quickstart-provider
COPY deploy/chart/files/schemas /etc/kedge/schemas
EXPOSE 8081
ENV PORT=8081
USER nonroot:nonroot
ENTRYPOINT ["/quickstart-provider"]
