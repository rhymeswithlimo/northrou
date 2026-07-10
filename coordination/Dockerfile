# Northrou coordination server, a tiny stateless WebRTC signaling relay.
# Build from the repository root:
#   docker build -f coordination/Dockerfile -t northrou-coordinator .

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY coordination/go.mod coordination/go.sum ./coordination/
RUN cd coordination && go mod download
COPY coordination ./coordination
RUN cd coordination && CGO_ENABLED=0 go build -ldflags "-s -w" -o /out/coordinator ./cmd/coordinator

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/coordinator /coordinator
ENV COORD_ADDR=:9000
EXPOSE 9000
ENTRYPOINT ["/coordinator"]
