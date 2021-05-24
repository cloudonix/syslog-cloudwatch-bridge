package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"bytes"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	uuid "github.com/satori/go.uuid"

	"gopkg.in/mcuadros/go-syslog.v2"
	"gopkg.in/mcuadros/go-syslog.v2/format"
)

var port = os.Getenv("PORT")
var logGroupName = os.Getenv("LOG_GROUP_NAME")
var streamName = os.Getenv("STREAM_NAME")
var sequenceToken = ""
var tickerTime = os.Getenv("TICKER_TIME")

var (
	client *http.Client
	pool   *x509.CertPool
)

func init() {
	pool = x509.NewCertPool()
	pool.AppendCertsFromPEM(pemCerts)
}

func main() {
	if logGroupName == "" {
		log.Fatal("LOG_GROUP_NAME must be specified")
	}

	if port == "" {
		port = "514"
	}

	uuid, err := uuid.NewV4()
	if err != nil {
		log.Fatalf("failed to generate UUID due to error: %v", err)
	}

	if streamName == "" {
		streamName = uuid.String()
	} else {
		streamName = streamName + "-" + uuid.String()
	}

	address := fmt.Sprintf("0.0.0.0:%v", port)
	log.Println("Starting syslog server on: ", address)
	log.Println("Logging to group: ", logGroupName)
	log.Println("Logging to stream: ", streamName)

	initCloudWatchStream()

	channel := make(syslog.LogPartsChannel, 100)
	handler := syslog.NewChannelHandler(channel)

	server := syslog.NewServer()
	server.SetFormat(syslog.Automatic)
	server.SetHandler(handler)
	err = server.ListenUDP(address)
	if err != nil {
		log.Fatalf("failed to listen on udp address %v due to error: %v", address, err)
	}

	err = server.ListenTCP(address)
	if err != nil {
		log.Fatalf("failed to listen on udp address %v due to error: %v", address, err)
	}

	err = server.Boot()
	if err != nil {
		log.Fatalf("failed to boot due to error: %v", err)
	}

	go func(channel syslog.LogPartsChannel) {
		ms := 200

		if tickerTime != "" {
			ms, _ = strconv.Atoi(tickerTime)
		}

		log.Printf("Ticker: %v milliseconds", ms)

		ticker := time.NewTicker(time.Duration(ms) * time.Millisecond)
		defer ticker.Stop() // release when done, if we ever will

		loglist := make([]format.LogParts, 0)
		for {
			select {
			case <-ticker.C:
				if len(loglist) <= 0 {
					continue
				}
				sendToCloudWatch(loglist)
				loglist = make([]format.LogParts, 0)
			case logParts := <-channel:
				loglist = append(loglist, logParts)
			}
		}
	}(channel)

	server.Wait()
}

func sendToCloudWatch(buffer []format.LogParts) {

	defer func() {
		if recover() != nil {
			log.Println("Recovered from panic in sendToCloudWatch")
		}
	}()

	// service is defined at run time to avoid session expiry in long running processes
	var svc = cloudwatchlogs.New(session.New())
	// set the AWS SDK to use our bundled certs for the minimal container (certs from CoreOS linux)
	svc.Config.HTTPClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}

	params := &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String(logGroupName),
		LogStreamName: aws.String(streamName),
	}

	sort.Slice(buffer, func(i, j int) bool {
		return buffer[i]["timestamp"].(time.Time).Before(buffer[j]["timestamp"].(time.Time))
	})

	for _, logPart := range buffer {
		if m, ok := formatMessageContent(logPart); ok {
			params.LogEvents = append(params.LogEvents, &cloudwatchlogs.InputLogEvent{
				Message:   aws.String(m),
				Timestamp: aws.Int64(makeMilliTimestamp(logPart["timestamp"].(time.Time))),
			})
		}
	}

	// first request has no SequenceToken - in all subsequent request we set it
	if sequenceToken != "" {
		params.SequenceToken = aws.String(sequenceToken)
	}

	resp, err := svc.PutLogEvents(params)
	if err != nil {
		log.Println(err)
		if awsErr, ok := err.(awserr.Error); ok {
			// Get error details
			code := awsErr.Code()
			if code == cloudwatchlogs.ErrCodeInvalidSequenceTokenException {
				// Extract expected Sequence from the message
				tokens := strings.Split(awsErr.Message(), ":")
				if len(tokens) > 1 {
					sequenceToken = strings.TrimSpace(tokens[1])
				}
			}
		}
	}
	log.Printf("Pushed %v entries to CloudWatch", len(buffer))

	sequenceToken = *resp.NextSequenceToken
	log.Printf("NextSequenceToken: %v", sequenceToken)
}

func initCloudWatchStream() {
	// service is defined at run time to avoid session expiry in long running processes
	var svc = cloudwatchlogs.New(session.New())
	// set the AWS SDK to use our bundled certs for the minimal container (certs from CoreOS linux)
	svc.Config.HTTPClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}

	_, err := svc.CreateLogStream(&cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String(logGroupName),
		LogStreamName: aws.String(streamName),
	})

	if err != nil {
		log.Fatal(err)
	}

	log.Println("Created CloudWatch Logs stream:", streamName)
}

func makeMilliTimestamp(input time.Time) int64 {
	return input.UnixNano() / int64(time.Millisecond)
}

//Receives the logParts map and returns the string message in format <hostname> <tag/app_name> [<proc_id>]: <content>
func formatMessageContent(message format.LogParts) (result string, ok bool) {
	var buffer bytes.Buffer
	content := message["message"]
	if content == nil || content == " " {
		content = message["content"]
	}
	if content == nil || content == "" {
		return "", false
	}
	if message["hostname"] != nil && message["hostname"] != "" {
		buffer.WriteString(message["hostname"].(string))
		buffer.WriteString(" ")
	}
	if message["tag"] != nil && message["tag"] != "" {
		buffer.WriteString(message["tag"].(string))
	} else if message["app_name"] != nil && message["app_name"] != "" {
		buffer.WriteString(message["app_name"].(string))
	} else {
		buffer.WriteString("-")
	}
	buffer.WriteString(" ")
	pid := message["proc_id"]
	if pid == nil || pid == "" || pid == "-" {
		pid = message["pid"]
	}
	if pid != nil && pid != "" && pid != "-" {
		buffer.WriteString("[")
		buffer.WriteString(pid.(string))
		buffer.WriteString("]:")
		buffer.WriteString(" ")
	}
	buffer.WriteString(message["message"].(string))
	return buffer.String(), true
}
