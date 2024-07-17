package request

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	pb "zephos/funnelbase/api"
)

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

func QueueRequest(req *pb.Request) (*pb.Response, error) {
	if err := ValidateRequest(req); err != nil {
		return nil, err
	}

	log.Printf("Received: %v", req.GetUrl())

	resp, err := MakeRequest(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)

	var respHeaders []*pb.Headers

	for key, values := range resp.Header {
		respHeaders = append(respHeaders, &pb.Headers{
			Key:   key,
			Value: values[0],
		})
	}

	return &pb.Response{
		StatusCode: int32(resp.StatusCode),
		Body:       string(body),
		Headers:    respHeaders,
	}, nil
}

func MakeRequest(req *pb.Request) (*http.Response, error) {
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

	return client.Do(httpReq)
}
