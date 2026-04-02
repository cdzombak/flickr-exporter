# Makefile for flickr-exporter

# Binary name
BINARY = flickr-exporter
BIN_VERSION := $(shell ./.version.sh)

# Go parameters
GOCMD = go
GOBUILD = $(GOCMD) build
GOCLEAN = $(GOCMD) clean

# Build target
all: build

build:
	$(GOBUILD) -ldflags="-X main.version=$(BIN_VERSION)" -o $(BINARY) -v

# Clean target - removes only the binary, not downloaded photos
clean:
	$(GOCLEAN)
	rm -f $(BINARY)

.PHONY: all build clean
