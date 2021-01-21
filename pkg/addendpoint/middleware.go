package addendpoint

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/endpoint" //endpoint(接口)
	"github.com/go-kit/kit/log"      //日志
	"github.com/go-kit/kit/metrics"  //metrics
)

func InstrumentingMiddleware(duration metrics.Histogram) endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, request interface{}) (response interface{}, err error) {
			defer func(begin time.Time) {
				duration.With("success", fmt.Sprint(err == nil)).Observe(time.Since(begin).Seconds())
			}(time.Now())

			return next(ctx, request)
		}
	}
}

func LoggingMiddleware(logger log.Logger) endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, request interface{}) (response interface{}, err error) {
			defer func(begin time.Time) {
				logger.Log("transport_error", err, "took", time.Since(begin))
			}(time.Now())

			return next(ctx, request)
		}
	}
}
