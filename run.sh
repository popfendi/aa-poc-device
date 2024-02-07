#!/usr/bin/env sh

export SERVER_ADDRESS=localhost:8008
export DEVICE_ID=IoTDevice123
export DB_OFFSET=10

go run main.go analyzer.go logger.go