# Start from a Debian image with the latest version of Go installed
# and a workspace (GOPATH) configured at /go.
FROM golang:alpine

# Build the outyet command inside the container.
# (You may fetch or manage dependencies here,
# either manually or with a tool like "godep".)
RUN apk add --no-cache git
RUN apk add --no-cache alpine-sdk
RUN apk add --no-cache zlib-dev
RUN apk add --no-cache bash
RUN git clone https://github.com/edenhill/librdkafka.git && \
    cd librdkafka && \
    ./configure --prefix /usr && \
    make && \
    sudo make install
ENV PKG_CONFIG_PATH=/go/src/github.com/flabbergasted/kafka
RUN go get github.com/gorilla/websocket
RUN go get github.com/confluentinc/confluent-kafka-go/kafka

# Copy the local package files to the container's workspace.
ENV HTML_LOCATION=/go/src/github.com/flabbergasted/kafka/html
ENV BROKER_LIST="localhost:9092, localhost:9093"
ADD . /go/src/github.com/flabbergasted/kafka
RUN go install github.com/flabbergasted/kafka

# Run the outyet command by default when the container starts.
ENTRYPOINT /go/bin/kafka

# Document that the service listens on port 1588.
EXPOSE 1588
