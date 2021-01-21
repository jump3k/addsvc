package addservice

import (
	"context"
	"errors"

	"github.com/go-kit/kit/log"     //日志
	"github.com/go-kit/kit/metrics" //性能指标
)

// 定义服务接口
type Service interface {
	Sum(ctx context.Context, a, b int) (int, error)
	Concat(ctx context.Context, a, b string) (string, error)
}

func New(logger log.Logger, ints, chars metrics.Counter) Service {
	var svc Service
	{
		svc = NewBasicService()                         //真正的业务逻辑
		svc = LoggingMiddleware(logger)(svc)            //日志
		svc = InstrumentingMiddleware(ints, chars)(svc) //metrics
	}
	return svc
}

func NewBasicService() Service {
	return basicService{}
}

type basicService struct{} //实现了Service定义的接口: SUM和Concat

func (s basicService) Sum(_ context.Context, a, b int) (int, error) {
	if a == 0 && b == 0 {
		return 0, ErrTwoZeroes
	}

	if (b > 0 && a > (intMax-b)) || (b < 0 && a < (intMin-b)) {
		return 0, ErrIntOverflow
	}

	return a + b, nil
}

func (s basicService) Concat(_ context.Context, a, b string) (string, error) {
	if len(a)+len(b) > maxLen {
		return "", ErrMaxSizeExceeded
	}

	return a + b, nil
}

var (
	intMax = 1<<31 - 1
	intMin = -(intMax + 1)
	maxLen = 10
)

var (
	ErrTwoZeroes       = errors.New("can't sum two zeroes")
	ErrIntOverflow     = errors.New("integer overflow")
	ErrMaxSizeExceeded = errors.New("result exceeds maximum size")
)
