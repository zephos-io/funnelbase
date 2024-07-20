package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	pb "zephos/funnelbase/api"
)

type Response struct {
	StatusCode int
	Body       string
	Headers    http.Header
	Request    *http.Request
	CacheHit   bool
}

func ValidateRequest(request *pb.Request) error {
	if request.Url == "" {
		return fmt.Errorf("url is required")
	}

	_, err := url.ParseRequestURI(request.Url)
	if err != nil {
		return fmt.Errorf("url is invalid")
	}

	return nil
}

func QueueRequest(req *pb.Request) (*Response, error) {
	if err := ValidateRequest(req); err != nil {
		return nil, err
	}

	resp, err := MakeRequest(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (resp *Response) ConvertResponseToGRPC() (*pb.Response, error) {
	var respHeaders []*pb.Headers

	for key, values := range resp.Headers {
		respHeaders = append(respHeaders, &pb.Headers{
			Key:   key,
			Value: values[0],
		})
	}

	return &pb.Response{
		//StatusCode: int32(resp.StatusCode),
		Body: resp.Body,
		//Headers:    respHeaders,
		CacheHit: resp.CacheHit,
	}, nil
}

func MakeRequest(req *pb.Request) (*Response, error) {
	client := &http.Client{}

	u := req.Url
	method := req.Method.String()

	httpReq, _ := http.NewRequest(method, u, nil)

	for _, header := range req.Headers {
		httpReq.Header.Add(header.Key, header.Value)
	}

	if req.Authorization != "" {
		httpReq.Header.Add("Authorization", req.Authorization)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}

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
