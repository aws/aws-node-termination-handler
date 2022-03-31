FROM golang:1.16

RUN go install github.com/client9/misspell/cmd/misspell@v0.3.4

CMD [ "/go/bin/misspell" ]