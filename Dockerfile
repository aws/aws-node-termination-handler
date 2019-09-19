# Build the manager binary
FROM golang:1.12.10 as builder

# Copy in the go src
WORKDIR /node-termination-handler
COPY . /node-termination-handler

# Build
RUN GO111MODULE="on" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o handler /node-termination-handler

# Copy the controller-manager into a thin image
FROM amazonlinux:2 as amazonlinux
FROM scratch
WORKDIR /
COPY --from=builder /node-termination-handler/handler .
COPY --from=amazonlinux /etc/ssl/certs/ca-bundle.crt /etc/ssl/certs/
COPY THIRD_PARTY_LICENSES .
ENTRYPOINT ["/handler"]
