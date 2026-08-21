# syntax=docker/dockerfile:1

FROM golang:1.27.0-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/spike ./cmd/spike

FROM debian:bookworm-slim
RUN useradd --system --no-create-home spike
COPY --from=build /out/spike /usr/local/bin/spike
USER spike
ENV SPIKE_DB=/tmp/spike.db
EXPOSE 8421
ENTRYPOINT ["/usr/local/bin/spike"]
