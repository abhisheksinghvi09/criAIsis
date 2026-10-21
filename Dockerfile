# API image. Static binary on a distroless, non-root base: no shell, no package
# manager, nothing to patch but the binary.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/criaisis ./cmd/criaisis

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/criaisis /criaisis
EXPOSE 8080
USER nonroot:nonroot
# Probes: GET /healthz (liveness) and /readyz (database reachable). Distroless has
# no curl, so wire them as orchestrator httpGet probes rather than HEALTHCHECK.
ENTRYPOINT ["/criaisis"]
