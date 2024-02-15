#!/usr/bin/env sh

export SERVER_ADDRESS=localhost:8008
export DEVICE_ID=IoTDevice123
export DB_OFFSET=10

go run main.go analyzer.go logger.go

# CGO_LDFLAGS="-latomic" CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=6 go build