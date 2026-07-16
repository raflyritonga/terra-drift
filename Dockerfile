# Builds the MCP server image. The client runs in CI, not as a container,
# so this ships terra-drift-mcp only.
# Match the builder tag to the go directive in go.mod.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/terra-drift-mcp ./cmd/terra-drift-mcp

# distroless static: no shell, no package manager, runs as nonroot, ships CA
# certs so the server can reach an HTTPS model endpoint.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/terra-drift-mcp /usr/local/bin/terra-drift-mcp
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/terra-drift-mcp"]
