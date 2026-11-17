FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY worker ./worker
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /out/api ./api && CGO_ENABLED=0 go build -o /out/worker ./worker

FROM alpine:3.22 AS api
COPY --from=build /out/api /usr/local/bin/api
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]

FROM alpine:3.22 AS worker
COPY --from=build /out/worker /usr/local/bin/worker
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/worker"]
