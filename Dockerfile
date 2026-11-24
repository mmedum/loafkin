# The production image: a static binary on distroless, run as nonroot.
# Each base is pinned by its multi-arch index digest, which Dependabot
# bumps; `make check` holds the Go tag equal to go.mod's.
FROM docker.io/library/golang:1.27.4@sha256:3102e2c7eeee6e16de01ca3f35c56434696f70702a30b3d564b0244560981bb0 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/loafkin ./cmd/loafkin

# The base carries CA certificates for outbound TLS and a nonroot user.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
COPY --from=build /out/loafkin /loafkin
EXPOSE 8080
ENTRYPOINT ["/loafkin"]
CMD ["serve"]
