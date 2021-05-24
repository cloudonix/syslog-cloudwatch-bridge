FROM golang as builder
WORKDIR /workspace
COPY go.mod ./
RUN go mod download -x
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o syslog-cloudwatch-bridge .

FROM scratch

EXPOSE 514
EXPOSE 514/udp

COPY --from=builder /workspace/syslog-cloudwatch-bridge /
CMD ["/syslog-cloudwatch-bridge"]
