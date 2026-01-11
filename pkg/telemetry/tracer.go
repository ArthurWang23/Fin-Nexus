package telemetry

import (
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"log"
)

func InitTracer(serviceName string, jaegerEndpoint string) func(ctx context.Context) error {
	ctx := context.Background()

	// 创建 OTLP HTTP Exporter（ 发送给Jaeger ）
	// 在 Docker 中，Endpoint 通常是 "jaeger:4318"
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(jaegerEndpoint), otlptracehttp.WithInsecure())
	if err != nil {
		log.Fatalf("failed to create trace exporter: %v", err)
	}

	// 2. 创建 Resource (标识当前服务)
	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(serviceName)))
	if err != nil {
		log.Fatalf("failed to create resource: %v", err)
	}

	// 创建 TraceProvider
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
	// 4. 设置全局 Provider
	otel.SetTracerProvider(tp)
	// 设置上下文传播 (Context Propagation)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp.Shutdown
}
