package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	lightstep "github.com/lightstep/lightstep-tracer-go"
	"github.com/oklog/oklog/pkg/group"
	stdopentracing "github.com/opentracing/opentracing-go"
	zipkinot "github.com/openzipkin-contrib/zipkin-go-opentracing"
	zipkin "github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/reporter/http"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"sourcegraph.com/sourcegraph/appdash"
	appdashot "sourcegraph.com/sourcegraph/appdash/opentracing"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/prometheus"
	kitgrpc "github.com/go-kit/kit/transport/grpc"

	"addsvc/pb"
	"addsvc/pkg/addendpoint"
	"addsvc/pkg/addservice"
	"addsvc/pkg/addtransport"
)

func main() {
	// 命令行参数
	fs := flag.NewFlagSet("addsvc", flag.ExitOnError)
	var (
		debugAddr = fs.String("debug.addr", ":8080", "Debug and metrics listen address")
		httpAddr  = fs.String("http-addr", ":8081", "HTTP listen address")
		grpcAddr  = fs.String("grpc-addr", ":8082", "gRPC listen address")
		//thriftAddr     = fs.String("thrift-addr", ":8083", "Thrift listen address")
		//jsonRPCAddr    = fs.String("jsonrpc-addr", ":8084", "JSON RPC listen address")
		//thriftProtocol = fs.String("thrift-protocol", "binary", "binary, compact, json, simplejson")
		//thriftBuffer   = fs.Int("thrift-buffer", 0, "0 for unbuffered")
		//thriftFramed   = fs.Bool("thrift-framed", false, "true to enable framing")
		zipkinURL      = fs.String("zipkin-url", "", "Enable Zipkin tracing via HTTP reporter URL e.g. http://localhost:9411/api/v2/spans")
		zipkinBridge   = fs.Bool("zipkin-ot-bridge", false, "Use Zipkin OpenTracing bridge instead of native implementation")
		lightstepToken = fs.String("lightstep-token", "", "Enable LightStep tracing via a LightStep access token")
		appdashAddr    = fs.String("appdash-addr", "", "Enable Appdash tracing via an Appdash server host:port")
	)
	fs.Usage = usageFor(fs, os.Args[0]+" [flags]")
	_ = fs.Parse(os.Args[1:])

	// 日志
	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr)                  //日志输出到stderr
		logger = log.With(logger, "ts", log.DefaultTimestampUTC) //ts时间字段
		logger = log.With(logger, "caller", log.DefaultCaller)   //caller字段
	}

	// 分布式追踪zipkin tracker
	var zipkinTracer *zipkin.Tracer
	{
		if *zipkinURL != "" {
			var (
				err         error
				hostPort    = "localhost:80"
				serviceName = "addsvc"
				reporter    = zipkinhttp.NewReporter(*zipkinURL)
			)
			defer reporter.Close()
			zEP, _ := zipkin.NewEndpoint(serviceName, hostPort)
			zipkinTracer, err = zipkin.NewTracer(reporter, zipkin.WithLocalEndpoint(zEP))
			if err != nil {
				_ = logger.Log("err", err)
				os.Exit(1)
			}
			if !(*zipkinBridge) {
				_ = logger.Log("tracer", "Zipkin", "type", "Native", "URL", *zipkinURL)
			}
		}
	}

	// 决定使用哪一种tracer
	var tracer stdopentracing.Tracer
	{
		if *zipkinBridge && zipkinTracer != nil {
			_ = logger.Log("tracer", "Zipkin", "type", "OpenTracing", "URL", *zipkinURL)
			tracer = zipkinot.Wrap(zipkinTracer)
			zipkinTracer = nil //使用opentracing bridge
		} else if *lightstepToken != "" {
			_ = logger.Log("tracer", "LightStep")
			tracer = lightstep.NewTracer(lightstep.Options{
				AccessToken: *lightstepToken,
			})
			defer lightstep.FlushLightStepTracer(tracer)
		} else if *appdashAddr != "" {
			_ = logger.Log("tracer", "Appdash", "addr", *appdashAddr)
			tracer = appdashot.NewTracer(appdash.NewRemoteCollector(*appdashAddr))
		} else {
			tracer = stdopentracing.GlobalTracer() // no-op
		}
	}

	// 创建度量指标
	var ints, chars metrics.Counter
	{
		// 业务层指标
		ints = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: "example",                                            //命名空间
			Subsystem: "addsvc",                                             //服务名称(子系统)
			Name:      "integers_summed",                                    //指标名字
			Help:      "Total count of integers summed via the Sum method.", //指标说明
		}, []string{})
		chars = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: "example",                                                       //命名空间
			Subsystem: "addsvc",                                                        //服务名称(子系统)
			Name:      "characters_concatenated",                                       //指标名字
			Help:      "Total count of characters concatenated via the Concat method.", //指标说明
		}, []string{})
	}
	var duration metrics.Histogram //直方图
	{
		// endpoint级别指标
		duration = prometheus.NewSummaryFrom(stdprometheus.SummaryOpts{
			Namespace: "example",                     //命名空间
			Subsystem: "addsvc",                      //服务名称(子系统)
			Name:      "request_duration_seconds",    //指标名字
			Help:      "Request duration in seconds", //指标说明
		}, []string{"method", "success"})
	}
	http.DefaultServeMux.Handle("/metrics", promhttp.Handler())

	var (
		service     = addservice.New(logger, ints, chars)                                  //服务(具体逻辑+中间件注入)
		endpoints   = addendpoint.New(service, logger, duration, tracer, zipkinTracer)     //Enpoint wrap service
		httpHandler = addtransport.NewHTTPHandler(endpoints, tracer, zipkinTracer, logger) //http handler wrap endpoint
		grpcServer  = addtransport.NewGRPCServer(endpoints, tracer, zipkinTracer, logger)  //grpc handler wrap endpoint
		//thriftServer = ...
		//jsonrpcHandler = ...
	)

	var g group.Group
	{
		debugListener, err := net.Listen("tcp", *debugAddr)
		if err != nil {
			_ = logger.Log("transport", "debug/HTTP", "during", "Listen", "err", err)
			os.Exit(1)
		}
		g.Add(func() error {
			_ = logger.Log("transport", "debug/HTTP", "addr", *debugAddr)
			return http.Serve(debugListener, http.DefaultServeMux)
		}, func(error) {
			debugListener.Close()
		})
	}
	{
		httpListener, err := net.Listen("tcp", *httpAddr)
		if err != nil {
			_ = logger.Log("transport", "HTTP", "during", "Listen", "err", err)
			os.Exit(1)
		}
		g.Add(func() error {
			_ = logger.Log("transport", "HTTP", "addr", *httpAddr)
			return http.Serve(httpListener, httpHandler)
		}, func(error) {
			httpListener.Close()
		})
	}
	{
		grpcListener, err := net.Listen("tcp", *grpcAddr)
		if err != nil {
			_ = logger.Log("transport", "gRPC", "during", "Listen", "err", err)
			os.Exit(1)
		}
		g.Add(func() error {
			_ = logger.Log("transport", "gRPC", "addr", *grpcAddr)
			baseServer := grpc.NewServer(grpc.UnaryInterceptor(kitgrpc.Interceptor))
			pb.RegisterAddServer(baseServer, grpcServer)
			return baseServer.Serve(grpcListener)
		}, func(error) {
			grpcListener.Close()
		})
	}

	{
		cancelInterrupt := make(chan struct{})
		g.Add(func() error {
			c := make(chan os.Signal, 1)
			signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
			select {
			case sig := <-c:
				return fmt.Errorf("received signal %s", sig)
			case <-cancelInterrupt:
				return nil
			}
		}, func(error) {
			close(cancelInterrupt)
		})
	}
	_ = logger.Log("exit", g.Run())
}

func usageFor(fs *flag.FlagSet, short string) func() {
	return func() {
		fmt.Fprintf(os.Stderr, "USAGE\n")
		fmt.Fprintf(os.Stderr, " %s\n", short)
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "FLAGS\n")
		w := tabwriter.NewWriter(os.Stderr, 0, 2, 2, ' ', 0)
		fs.VisitAll(func(f *flag.Flag) {
			fmt.Fprintf(w, "\t-%s \t%s (default: %s)\n", f.Name, f.Usage, f.DefValue)
		})
		w.Flush()
		fmt.Fprintf(os.Stderr, "\n")
	}
}
