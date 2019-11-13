FROM golang:1

WORKDIR /app

RUN go get github.com/gojp/goreportcard
RUN cd $GOPATH/src/github.com/gojp/goreportcard && make install

RUN go get github.com/gojp/goreportcard/cmd/goreportcard-cli

CMD $GOPATH/bin/goreportcard-cli -v -t 90
