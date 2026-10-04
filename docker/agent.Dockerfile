# shrooms-agent alone, for scripts/install-agent.sh to copy out (docs/agents.md).
#
# Its own image, not a layer of the shrooms one: nodes follow
# ghcr.io/vpavlin/shrooms:latest with podman auto-update, so publishing the
# agent there would roll a new daemon onto every one of them. Nothing runs
# this image; the agent runs on the host, as a user. Pure Go and static, so
# one binary per architecture and nothing else.
FROM scratch
ARG TARGETARCH
COPY shrooms-agent-${TARGETARCH} /usr/bin/shrooms-agent
ENTRYPOINT ["/usr/bin/shrooms-agent"]
