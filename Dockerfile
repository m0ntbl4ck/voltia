FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# timetzdata embeds the zone database so PLANT_TZ works in an image without one.
RUN CGO_ENABLED=0 go build -trimpath -tags timetzdata -ldflags="-s -w" -o /out/voltia ./cmd/voltia

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/voltia /voltia
EXPOSE 8080
ENTRYPOINT ["/voltia"]
