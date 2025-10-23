FROM golang:1.24 AS build
WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN go mod tidy

RUN CGO_ENABLED=0 GOOS=linux go build -o /publisher

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build /publisher /publisher
ENTRYPOINT ["/publisher"]