# Portico autonomous-build container.
# Go 1.25 toolchain + Node 22 (SvelteKit Console) + golangci-lint (CI-pinned)
# + GitHub CLI + Bun + opencode. Build target is CGo-free (CGO_ENABLED=0),
# a single static binary — the container only needs to compile and boot it.
FROM golang:1.25-bookworm

ENV DEBIAN_FRONTEND=noninteractive

# Base tooling
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl git unzip ripgrep jq build-essential \
    && rm -rf /var/lib/apt/lists/*

# Node.js 22 LTS + npm — for the SvelteKit + Vite Console UI (web/console).
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/* \
    && npm --version && node --version

# GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# golangci-lint pinned to the CI version (.github/workflows/ci.yml) so an
# in-container `make lint` matches the merge gate exactly.
RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8

# Bun (opencode plugin installer) + opencode CLI
ENV BUN_INSTALL=/usr/local/bun
ENV PATH=$BUN_INSTALL/bin:/root/.opencode/bin:$PATH
RUN curl -fsSL https://bun.sh/install | bash
RUN curl -fsSL https://opencode.ai/install | bash

# Toolchain bins on PATH for login/interactive shells too — the loop runs `bash -lc`,
# which sources /etc/profile.d and would otherwise reset PATH and lose the toolchain.
RUN printf 'export PATH="/usr/local/bun/bin:/root/.opencode/bin:/usr/local/go/bin:/go/bin:/usr/local/bin:$PATH"\n' \
        > /etc/profile.d/portico.sh

# Global opencode config: plugins + the /goal command. No secrets baked in.
RUN mkdir -p /root/.config/opencode
COPY opencode-global.json /root/.config/opencode/opencode.json

WORKDIR /workspace/portico

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["sleep", "infinity"]
