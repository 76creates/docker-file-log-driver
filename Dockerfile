FROM  golang:1.18.0 as builder

COPY . /go/src/github.com/trickest/twe/agent/agent/runner/docker/log-driver
RUN cd /go/src/github.com/trickest/twe/agent/agent/runner/docker/log-driver && make

FROM debian:buster-slim
COPY --from=builder /usr/bin/docker-log-driver /usr/bin/docker-log-driver