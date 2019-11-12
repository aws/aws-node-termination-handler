# Build the manager binary
FROM golang:1.13 as builder

## GOLANG env
ARG GO111MODULE="on"
ARG CGO_ENABLED=0
ARG GOOS=linux 
ARG GOARCH=amd64 

# Copy go.mod and download dependencies
WORKDIR /node-termination-handler
COPY go.mod .
RUN go mod download

# Build
COPY . /node-termination-handler
RUN go build -a -o handler /node-termination-handler

# Copy the controller-manager into a thin image
FROM amazonlinux:2 as amazonlinux
FROM scratch
WORKDIR /
COPY --from=builder /node-termination-handler/handler .
COPY --from=amazonlinux /etc/ssl/certs/ca-bundle.crt /etc/ssl/certs/
COPY THIRD_PARTY_LICENSES .
ENTRYPOINT ["/handler"]
