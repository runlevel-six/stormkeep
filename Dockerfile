# The dashboard. CGO_ENABLED=0 keeps the binary static, which is what lets the
# runtime stage be scratch: the SQLite driver is pure Go for that reason, and
# the time zone database is compiled in (time/tzdata).
FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# Stamped into the binary: `stormkeep version` and the page footer
# report it, and every static asset URL carries it.
ARG VERSION=dev

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/stormkeep ./cmd/stormkeep

FROM scratch
COPY --from=build /out/stormkeep /stormkeep
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/stormkeep"]
