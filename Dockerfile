# Build from workspace parent (needs open-message + clients + oxia + openkv-client-go):
#   docker build -f open-message/Dockerfile -t open-message:local .
FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY open-ledger-client-go /open-ledger-client-go
COPY open-message-client-go /open-message-client-go
COPY oxia /oxia
COPY amydemo/openkv-client-go /openkv-client-go
COPY open-message /src/open-message
WORKDIR /src/open-message
RUN sed -i 's|=> ../open-ledger-client-go|=> /open-ledger-client-go|' go.mod \
 && sed -i 's|=> ../open-message-client-go|=> /open-message-client-go|' go.mod \
 && sed -i 's|=> ../oxia/oxia|=> /oxia/oxia|' go.mod \
 && sed -i 's|=> ../oxia/common|=> /oxia/common|' go.mod \
 && sed -i 's|=> ../amydemo/openkv-client-go|=> /openkv-client-go|' go.mod \
 && GOTOOLCHAIN=auto go mod download \
 && CGO_ENABLED=0 GOTOOLCHAIN=auto go build -o /out/broker ./cmd/broker \
 && CGO_ENABLED=0 GOTOOLCHAIN=auto go build -o /out/proxy ./cmd/proxy \
 && CGO_ENABLED=0 GOTOOLCHAIN=auto go build -o /out/metakv ./cmd/metakv \
 && CGO_ENABLED=0 GOTOOLCHAIN=auto go build -o /out/omexample ./examples/produce-consume

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/broker /usr/local/bin/broker
COPY --from=build /out/proxy /usr/local/bin/proxy
COPY --from=build /out/metakv /usr/local/bin/metakv
COPY --from=build /out/omexample /usr/local/bin/omexample
USER nonroot:nonroot
