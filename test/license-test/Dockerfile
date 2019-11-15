FROM golang:1

WORKDIR /app

COPY license-config.hcl .
ARG GOPROXY="https://proxy.golang.org,direct"
RUN GO111MODULE=on go get github.com/mitchellh/golicense

CMD $GOPATH/bin/golicense 
