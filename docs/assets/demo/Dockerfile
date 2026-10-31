# Image for recording docs/assets/demo.gif. Build it from the repository root:
#   docker build -f docs/assets/demo/Dockerfile -t spmeta-demo .
# See record.sh, which builds the image and runs the recording.

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -o /out/spmeta ./cmd/spmeta \
 && go build -trimpath -tags demo -o /out/mockplayer ./docs/assets/demo/mockplayer

# Keep v0.11.0: v0.12.0 exits without writing the GIF in this setup.
FROM ghcr.io/charmbracelet/vhs:v0.11.0
RUN apt-get update \
 && apt-get install -y --no-install-recommends dbus-daemon jq \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/spmeta /out/mockplayer /usr/local/bin/
COPY docs/assets/demo/session.sh /usr/local/bin/demo-session
ENTRYPOINT ["dbus-run-session", "--", "demo-session"]
