FROM golang:1

WORKDIR /app

RUN go get github.com/gojp/goreportcard
RUN cd $GOPATH/src/github.com/gojp/goreportcard && (make install || cd / && curl -L https://git.io/vp6lP | sh)

RUN go get github.com/gojp/goreportcard/cmd/goreportcard-cli

RUN go get -u golang.org/x/tools/cmd/goimports

CMD $GOPATH/bin/goreportcard-cli -v -t 90
