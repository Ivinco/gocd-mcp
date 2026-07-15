# --- build ---
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gocd-mcp ./cmd/gocd-mcp

# --- run ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/gocd-mcp /usr/local/bin/gocd-mcp
EXPOSE 8443
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/gocd-mcp"]
