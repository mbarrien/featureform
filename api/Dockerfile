# docker buildx build -f ./api/Dockerfile . -t featureformcom/api-server:latest -o type=image --platform=linux/arm64,linux/amd64 --push
FROM golang:1.18-alpine

WORKDIR /app

COPY go.mod ./
COPY go.sum ./

COPY ./metadata/proto/metadata.proto ./metadata/proto/metadata.proto
COPY ./proto/ ./proto/
RUN apk update && apk add protobuf-dev && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest && go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
ENV PATH /go/bin:$PATH
RUN protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative ./metadata/proto/metadata.proto
RUN protoc --go_out=. --go_opt=paths=source_relative     --go-grpc_out=. --go-grpc_opt=paths=source_relative     ./proto/serving.proto

COPY ./metadata/*.go ./metadata/
COPY ./metadata/search/ ./metadata/search/
COPY ./metadata/proto/ ./metadata/proto/
COPY ./proto/ ./proto/
COPY ./helpers/ ./helpers/
COPY ./api/main.go ./api/main.go
COPY ./provider/provider_config/ ./provider/provider_config/
COPY ./provider/provider_type/ ./provider/provider_type/
COPY ./config/ ./config/

RUN go build ./api/main.go

FROM alpine

COPY --from=0 ./app/main ./main

EXPOSE 8080
ENTRYPOINT ["./main"]