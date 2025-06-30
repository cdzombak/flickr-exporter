# Makefile for flickr-exporter

# Binary name
BINARY = flickr-exporter

# Go parameters
GOCMD = go
GOBUILD = $(GOCMD) build
GOCLEAN = $(GOCMD) clean

# Build target
all: build

build:
	$(GOBUILD) -o $(BINARY) -v

# Clean target - removes only the binary, not downloaded photos
clean:
	$(GOCLEAN)
	rm -f $(BINARY)

.PHONY: all build clean