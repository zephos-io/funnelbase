FROM golang:1.22.5 as builder

WORKDIR /usr/src/app

RUN apt-get update
RUN apt install -y protobuf-compiler

COPY . .

RUN go mod download && go mod verify

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go \
    google.golang.org/grpc/cmd/protoc-gen-go-grpc

RUN make generate-grpc

RUN CGO_ENABLED=0 GOOS=linux go build -v -o ./build/server.exe ./cmd/server

FROM alpine:3.20.2 as runtime
COPY --from=builder /usr/src/app/build /usr/src/app

WORKDIR /usr/src/app

CMD ["./server.exe"]
