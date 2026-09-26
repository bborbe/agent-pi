ARG DOCKER_REGISTRY=docker.prod.nuke.benjamin-borbe.de:443
FROM ${DOCKER_REGISTRY}/golang:1.27.1 AS build
ARG BUILD_GIT_VERSION=dev
ARG BUILD_GIT_COMMIT=none
ARG BUILD_DATE=unknown
COPY . /workspace
WORKDIR /workspace
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -mod=vendor -ldflags "-s" -a -installsuffix cgo -o /main
CMD ["/bin/bash"]

FROM ${DOCKER_REGISTRY}/alpine:3.24 AS alpine
# The pi CLI is pinned deliberately, and this pin is load-bearing.
#
# `pi --mode json` emits a stream of `{"type": ...}` events that the runner parses
# **by name**, and the names have changed once already: pi 0.87.x emits
# `message_end` where older builds emitted `agent_end`. Installed unpinned, an image
# rebuild silently adopts whatever vocabulary is current, and the runner then reports
# "no result found in pi CLI output" on runs that in fact succeeded — a symptom that
# reads as a model failure and is a parser mismatch. That is not hypothetical: it is
# what a v0.4.0 image did on 2026-09-26, and it was invisible because the fleet's
# working agents run older images.
#
# Bump this deliberately, and when you do, check `extractEventText` in
# github.com/bborbe/agent's pi/pi-runner.go against the new stream.
RUN apk --no-cache add ca-certificates curl bash nodejs npm \
 && npm install -g --omit=dev --no-optional @earendil-works/pi-coding-agent@0.87.1 \
 && npm cache clean --force \
 && apk del npm \
 && rm -rf /root/.npm /tmp/*

FROM alpine
ARG BUILD_GIT_VERSION=dev
ARG BUILD_GIT_COMMIT=none
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.version="${BUILD_GIT_VERSION}"
COPY --from=build /main /main
COPY agent/ /agent/
ENV HOME=/home/pi
RUN mkdir -p /home/pi/.pi
ENV ZONEINFO=/zoneinfo.zip
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /
# Copy pi CLI from the intermediate stage.
COPY --from=alpine /usr/local/bin/pi /usr/local/bin/pi
COPY --from=alpine /usr/local/lib/node_modules /usr/local/lib/node_modules
ENV PATH="/usr/local/bin:${PATH}"
ENV BUILD_GIT_VERSION=${BUILD_GIT_VERSION}
ENV BUILD_GIT_COMMIT=${BUILD_GIT_COMMIT}
ENV BUILD_DATE=${BUILD_DATE}
ENTRYPOINT ["/main", "-v=2"]
