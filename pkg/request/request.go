package request

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/pkg/services/prometheus"
)

type Response struct {
	StatusCode int
	Body       string
	Headers    http.Header
	Request    *http.Request
	CacheHit   bool
	RetryCount int
}

func ValidateRequest(request *pb.Request) error {
	if request.Url == "" {
		return fmt.Errorf("url is required")
	}

	if _, err := url.Parse(request.Url); err != nil {
		return fmt.Errorf("url is invalid")
	}

	return nil
}

// ConvertResponseToGRPC converts a given pb.Response to a suitable gRPC response to send back to the client
func (resp *Response) ConvertResponseToGRPC() (*pb.Response, error) {
	var respHeaders []*pb.Headers

	for key, values := range resp.Headers {
		respHeaders = append(respHeaders, &pb.Headers{
			Key:   key,
			Value: values[0],
		})
	}

	return &pb.Response{
		StatusCode: int32(resp.StatusCode),
		Body:       resp.Body,
		Headers:    respHeaders,
		CacheHit:   resp.CacheHit,
	}, nil
}

// Request handles the http request
func Request(ctx context.Context, req *pb.Request) (*Response, error) {
	client := &http.Client{}

	url := req.Url
	method := req.Method.String()

	var reqBody io.Reader = nil

	if req.Body != "" {
		reqBody = bytes.NewBufferString(req.Body)
	}

	// use the parents context to create a new context specifically for the request with a 30-second timeout
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, method, url, reqBody)
	if err != nil {
		return nil, err
	}

	for _, header := range req.Headers {
		httpReq.Header.Add(header.Key, header.Value)
	}

	if req.Authorization != "" {
		httpReq.Header.Add("Authorization", req.Authorization)
	}

	startReqTime := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}

	prometheus.OutboundRequests.WithLabelValues(req.RateLimit, req.Client).Observe(float64(time.Since(startReqTime).Milliseconds()))

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	bodyStr := string(body)

	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		buffer := new(bytes.Buffer)
		err := json.Compact(buffer, body)
		// if it fails to compact json (may not even be json)
		if err == nil {
			bodyStr = buffer.String()
		}
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Body:       bodyStr,
		Headers:    resp.Header,
		Request:    resp.Request,
		CacheHit:   false,
	}, nil
}
