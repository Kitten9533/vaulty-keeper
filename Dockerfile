# vaulty-keeper agent container: runs agent CLIs (codex / claude / opencode / pi)
# isolated from the host. The host runs 'vaulty-keeper serve' and holds the
# snapshot keys; this container only ever receives masked values through the
# bridge token. Keys and ciphertext never enter the container.
#
# Build:
#   docker build -t vaulty-keeper-agent:local .
#
# The Go binary is built from source in the first stage, so the host does not
# need to build anything beforehand.
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/vaulty-keeper .

FROM node:22-bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace

COPY --from=build /out/vaulty-keeper /usr/local/bin/vaulty-keeper
COPY docker/agent-entrypoint.sh /usr/local/bin/agent-entrypoint
RUN chmod +x /usr/local/bin/agent-entrypoint \
    && useradd --create-home --shell /bin/bash agent

USER agent

ENTRYPOINT ["/usr/local/bin/agent-entrypoint"]
CMD ["bash"]
