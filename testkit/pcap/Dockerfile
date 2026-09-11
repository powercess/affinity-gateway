# syntax=docker/dockerfile:1
#
# L4 capture image. It is run with network_mode: "service:<tap>" so it shares
# the tap's network namespace and records the exact bytes on the wire, which is
# the ground truth the L7 NDJSON is cross-checked against.
FROM alpine:3.21

RUN apk add --no-cache tcpdump

ENTRYPOINT ["tcpdump"]
