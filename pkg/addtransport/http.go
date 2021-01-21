package addtransport

import (
	"context"
	"encoding/json"
	"net/http"

	stdopentracing "github.com/opentracing/opentracing-go"
	stdzipkin "github.com/openzipkin/zipkin-go"

	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log" //log
	"github.com/go-kit/kit/tracing/opentracing"
	"github.com/go-kit/kit/tracing/zipkin"
	"github.com/go-kit/kit/transport"
	httptransport "github.com/go-kit/kit/transport/http" //http

	"addsvc/pkg/addendpoint"
	"addsvc/pkg/addservice"
)

func NewHTTPHandler(endpoints addendpoint.Set, otTracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, logger log.Logger) http.Handler {
	options := []httptransport.ServerOption{
		httptransport.ServerErrorEncoder(errorEncoder),
		httptransport.ServerErrorHandler(transport.NewLogErrorHandler(logger)),
	}

	if zipkinTracer != nil {
		options = append(options, zipkin.HTTPServerTrace(zipkinTracer))
	}

	m := http.NewServeMux()
	m.Handle("/sum", httptransport.NewServer(
		endpoints.SumEndPoint,     //指定endpoint
		decodeHTTPSumRequest,      //解码请求
		encodeHTTPGenericResponse, //编码响应
		append(options, httptransport.ServerBefore(opentracing.HTTPToContext(otTracer, "Sum", logger)))..., //追踪
	))
	m.Handle("/concat", httptransport.NewServer(
		endpoints.ConcatEndpoint,  //指定endpoint
		decodeHTTPConcatRequest,   //解码请求
		encodeHTTPGenericResponse, //编码响应
		append(options, httptransport.ServerBefore(opentracing.HTTPToContext(otTracer, "Sum", logger)))..., //追踪
	))

	return m
}

func decodeHTTPSumRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req addendpoint.SumRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	return req, err
}

func decodeHTTPConcatRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req addendpoint.ConcatRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	return req, err
}

func errorEncoder(_ context.Context, err error, w http.ResponseWriter) {
	w.WriteHeader(err2code(err))
	json.NewEncoder(w).Encode(errorWrapper{Error: err.Error()})
}

func err2code(err error) int {
	switch err {
	case addservice.ErrTwoZeroes, addservice.ErrMaxSizeExceeded, addservice.ErrIntOverflow:
		return http.StatusBadRequest //400
	}
	return http.StatusInternalServerError //500
}

type errorWrapper struct {
	Error string `json:"error"`
}

func encodeHTTPGenericResponse(ctx context.Context, w http.ResponseWriter, response interface{}) error {
	if f, ok := response.(endpoint.Failer); ok && f.Failed() != nil {
		errorEncoder(ctx, f.Failed(), w)
		return nil
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(response)
}
