#!/bin/sh

# protoc -I. api.proto --go_out=plugins=grpc:walletrpc

protoc -I. \
  --go_out=walletrpc --go_opt=paths=source_relative \
  --go-grpc_out=walletrpc --go-grpc_opt=paths=source_relative \
  api.proto