# Build the manager binary
FROM golang:1-alpine as builder

## GOLANG env
ARG GOPROXY="https://proxy.golang.org,direct"
ARG GO111MODULE="on"
ARG CGO_ENABLED=0
ARG GOOS=linux 
ARG GOARCH=amd64 

# Copy go.mod and download dependencies
WORKDIR /ec2-meta-data-proxy
COPY go.mod .
COPY go.sum .
RUN go mod download

# Build
COPY . .
RUN cd cmd && go build -a -o ec2-meta-data-proxy .
# In case the target is build for testing:
# $ docker build  --target=builder -t test .
ENTRYPOINT ["cmd/ec2-meta-data-proxy"]

# Copy the controller-manager into a thin image
FROM amazonlinux:2 as amazonlinux
FROM scratch
WORKDIR /
COPY --from=builder /ec2-meta-data-proxy/cmd/ec2-meta-data-proxy .
COPY --from=amazonlinux /etc/ssl/certs/ca-bundle.crt /etc/ssl/certs/
COPY THIRD_PARTY_LICENSES .
ENTRYPOINT ["/ec2-meta-data-proxy"]
