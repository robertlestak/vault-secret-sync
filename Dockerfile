# First stage: build the Go application
FROM golang:1.22.5 AS builder

# Install dependencies
RUN apt-get update -y && apt-get install -y curl build-essential unzip gcc make pkg-config

ENV LIBSODIUM_VERSION 1.0.18
RUN \
    mkdir -p /tmpbuild/libsodium && \
    cd /tmpbuild/libsodium && \
    curl -L https://download.libsodium.org/libsodium/releases/libsodium-${LIBSODIUM_VERSION}.tar.gz -o libsodium-${LIBSODIUM_VERSION}.tar.gz && \
    tar xfvz libsodium-${LIBSODIUM_VERSION}.tar.gz && \
    cd /tmpbuild/libsodium/libsodium-${LIBSODIUM_VERSION}/ && \
    ./configure && \
    make && make check && \
    make install && \
    mv src/libsodium /usr/local/ && \
    rm -Rf /tmpbuild/ && \
    ldconfig

# Set the Current Working Directory inside the container
WORKDIR /src

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy the source code into the container
COPY . .

# Run tests
RUN go test ./...

# Build the Go application
RUN go build -o /bin/vss cmd/vss/*.go

FROM ubuntu:22.04 as vss

WORKDIR /src

RUN apt-get update -y && apt-get install -y curl build-essential unzip gcc make pkg-config

ENV LIBSODIUM_VERSION 1.0.18
RUN \
    mkdir -p /tmpbuild/libsodium && \
    cd /tmpbuild/libsodium && \
    curl -L https://download.libsodium.org/libsodium/releases/libsodium-${LIBSODIUM_VERSION}.tar.gz -o libsodium-${LIBSODIUM_VERSION}.tar.gz && \
    tar xfvz libsodium-${LIBSODIUM_VERSION}.tar.gz && \
    cd /tmpbuild/libsodium/libsodium-${LIBSODIUM_VERSION}/ && \
    ./configure && \
    make && make check && \
    make install && \
    mv src/libsodium /usr/local/ && \
    rm -Rf /tmpbuild/ && \
    ldconfig

COPY --from=builder /bin/vss /bin/vss

# Command to run the executable
ENTRYPOINT [ "/bin/vss" ]
