package addtransport

import (
	"context"

	stdopentracing "github.com/opentracing/opentracing-go"
	stdzipkin "github.com/openzipkin/zipkin-go"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/tracing/opentracing"
	"github.com/go-kit/kit/tracing/zipkin"
	"github.com/go-kit/kit/transport"
	grpctransport "github.com/go-kit/kit/transport/grpc"

	"addsvc/pb"
	"addsvc/pkg/addendpoint"
)

type grpcServer struct {
	sum    grpctransport.Handler
	concat grpctransport.Handler
}

func NewGRPCServer(
	endpoints addendpoint.Set,
	otTracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer,
	logger log.Logger) pb.AddServer {

	options := []grpctransport.ServerOption{
		grpctransport.ServerErrorHandler(transport.NewLogErrorHandler(logger)),
	}

	if zipkinTracer != nil {
		options = append(options, zipkin.GRPCServerTrace(zipkinTracer))
	}

	return &grpcServer{
		sum: grpctransport.NewServer(
			endpoints.SumEndPoint,
			decodeGRPCSumRequest,
			encodeGRPCSumResponse,
			append(options, grpctransport.ServerBefore(opentracing.GRPCToContext(otTracer, "Sum", logger)))...,
		),
		concat: grpctransport.NewServer(
			endpoints.ConcatEndpoint,
			decodeGRPCConcatRequest,
			encodeGRPCConcatResponse,
			append(options, grpctransport.ServerBefore(opentracing.GRPCToContext(otTracer, "Concat", logger)))...,
		),
	}
}

func (s *grpcServer) Sum(ctx context.Context, req *pb.SumRequest) (*pb.SumReply, error) {
	_, rep, err := s.sum.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}

	return rep.(*pb.SumReply), nil
}

func (s *grpcServer) Concat(ctx context.Context, req *pb.ConcatRequest) (*pb.ConcatReply, error) {
	_, rep, err := s.concat.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}

	return rep.(*pb.ConcatReply), nil
}

func decodeGRPCSumRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.SumRequest)
	return addendpoint.SumRequest{A: int(req.A), B: int(req.B)}, nil
}

func encodeGRPCSumResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(addendpoint.SumResponse)
	return &pb.SumReply{V: int64(resp.V), Err: err2str(resp.Err)}, nil
}

func decodeGRPCConcatRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.ConcatRequest)
	return addendpoint.ConcatRequest{A: req.A, B: req.B}, nil
}

func encodeGRPCConcatResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(addendpoint.ConcatResponse)
	return &pb.ConcatReply{V: resp.V, Err: err2str(resp.Err)}, nil
}

func err2str(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}
