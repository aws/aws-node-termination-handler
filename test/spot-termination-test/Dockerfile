FROM golang:1-alpine as builder

WORKDIR /ec2-meta-data-proxy
COPY . /ec2-meta-data-proxy
RUN GO111MODULE="off" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o ec2-meta-data-proxy .

# Copy into thin image
FROM scratch
WORKDIR /
COPY --from=builder /ec2-meta-data-proxy/ec2-meta-data-proxy .
ENTRYPOINT ["/ec2-meta-data-proxy"]
