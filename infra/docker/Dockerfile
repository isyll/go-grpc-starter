# Build every binary, then copy them into a small runtime image.
FROM golang:1.27 AS build
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN LDFLAGS="-s -w \
      -X github.com/isyll/go-grpc-starter/pkg/version.version=${VERSION} \
      -X github.com/isyll/go-grpc-starter/pkg/version.commit=${COMMIT} \
      -X github.com/isyll/go-grpc-starter/pkg/version.date=${BUILD_DATE}" \
 && CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o /out/migrate ./cmd/migrate \
 && CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o /out/worker ./cmd/worker

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/ /app/
COPY configs ./configs
COPY migrations ./migrations
COPY locales ./locales
COPY templates ./templates
EXPOSE 8080 9090
ENTRYPOINT ["/app/server"]
