# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: build-api

build-api:
	go build -o bin/karina-ui main.go

build-webfont:
#TODO: handle installation
	fantasticon  --normalize true ./src/assets/svg/webfont-icon-source/ -o ./src/assets/fonts/karina-ui-icons