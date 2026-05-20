FROM node:22-bookworm

# Install system dependencies
RUN apt-get update && apt-get install -y \
  git \
  curl \
  jq \
  && rm -rf /var/lib/apt/lists/*

# Install GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
  | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
  && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
  | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
  && apt-get update && apt-get install -y gh \
  && rm -rf /var/lib/apt/lists/*

# Install Go 1.26.2
RUN curl -fsSL https://go.dev/dl/go1.26.2.linux-amd64.tar.gz | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"

# Install kubectl
RUN curl -fsSL "https://dl.k8s.io/release/$(curl -fsSL https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" \
  -o /usr/local/bin/kubectl \
  && chmod +x /usr/local/bin/kubectl

# Install Helm 3
RUN curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

# Install uv (Python package manager for SDK testing/linting)
COPY --from=ghcr.io/astral-sh/uv:python3.12-trixie /uv /usr/local/bin/uv
COPY --from=ghcr.io/astral-sh/uv:python3.12-trixie /uvx /usr/local/bin/uvx

# Install TypeScript globally so `tsc` is available for SDK type-checking
RUN npm install -g typescript

# Rename the base image's "node" user (UID 1000) to "agent".
# This keeps UID 1000 so that --userns=keep-id (Podman) and
# --user 1000:1000 (Docker) map to the correct home directory owner.
RUN usermod -d /home/agent -m -l agent node

# Install pi coding agent (run as root before USER agent)
RUN npm install -g @mariozechner/pi-coding-agent

USER agent

# Pre-install Python 3.12 via uv so SDK tests don't need a network fetch at runtime
RUN uv python install 3.12

# Install helm-unittest plugin (baked in so agents don't need network at test time)
RUN helm plugin install https://github.com/helm-unittest/helm-unittest.git

# Install otel exporter as agent user so it lands in /home/agent/.pi/
RUN pi install git:github.com/mprokopov/pi-otel-telemetry

# Bake in git author identity so Pi's commits are never rejected.
RUN git config --global user.name "Sandcastle Agent" \
  && git config --global user.email "agent@blosshomelab.com"

WORKDIR /home/agent

# In worktree sandbox mode, Sandcastle bind-mounts the git worktree at /home/agent/workspace
# and overrides the working directory to /home/agent/workspace at container start.
# Structure your Dockerfile so that /home/agent/workspace can serve as the project root.
ENTRYPOINT ["sleep", "infinity"]
