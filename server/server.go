package server

import (
	"context"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/request"
)

type Server struct {
	pb.UnimplementedFunnelbaseServer
}

func (s *Server) QueueRequest(ctx context.Context, req *pb.Request) (*pb.Response, error) {
	return request.QueueRequest(req)
}
