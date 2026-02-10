package main

import (
	"context"
	"flag"
	"log"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	deprecatedmodelspb "grpc-mock/internal/genproto/complex/deprecatedmodels"
	importmepb "grpc-mock/internal/genproto/complex/importme"
	modelspb "grpc-mock/internal/genproto/complex/models"
	servicepb "grpc-mock/internal/genproto/complex/service"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	addr = flag.String("addr", "localhost:50051", "the address to connect to")
)

func main() {
	flag.Parse()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := servicepb.NewComplexServiceClient(conn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	scenarios := []struct {
		rpsFunc func(float64) float64
		task    func(context.Context, servicepb.ComplexServiceClient)
	}{
		// GetModel Load
		{
			// Main sine wave traffic 50-150 RPS, 60s period
			rpsFunc: sineWave(100, 50, 120*time.Second, 0),
			task:    getModelFabric("first"),
		},
		{
			// High frequency low amplitude noise 20-40 RPS, 13s period
			rpsFunc: sineWave(30, 30, 26*time.Second, 0),
			task:    getModelFabric("second"),
		},
		{
			// Periodic errors 0-10 RPS, 20s period
			rpsFunc: sineWave(5, 15, 40*time.Second, 0),
			task:    getModelFabric("error: Alice"),
		},
		{
			// Periodic errors offset 0-6 RPS, 17s period
			rpsFunc: sineWave(3, 13, 34*time.Second, 5*time.Second),
			task:    getModelFabric("error: Bob"),
		},
		{
			// Periodic errors offset 0-4 RPS, 11s period
			rpsFunc: sineWave(5, 15, 22*time.Second, 2*time.Second),
			task:    getModelFabric("error: Carol"),
		},
		{
			// Rare panic, short spike every 60s
			rpsFunc: sineWave(2, 10, 120*time.Second, 0),
			task:    getModelFabric("panic: 42"),
		},
		{
			// Rare panic, short spike every 45s, offset
			rpsFunc: sineWave(1, 4, 90*time.Second, 30*time.Second),
			task:    getModelFabric("panic: 1000-7"),
		},
		// GetOldModel Load
		{
			// Steady sawtooth 40-60 RPS, 30s period
			rpsFunc: sawtoothWave(40, 20, 60*time.Second, 0),
			task:    getOldModelFabric("good"),
		},
		{
			// Periodic errors sawtooth 5-10 RPS, 20s period
			rpsFunc: sawtoothWave(5, 5, 40*time.Second, 0),
			task:    getOldModelFabric("error: Jesus Christ"),
		},
		{
			// Rare panic sawtooth 1-3 RPS, 60s period
			rpsFunc: sawtoothWave(1, 2, 120*time.Second, 0),
			task:    getOldModelFabric("panic: 666"),
		},
		// DoNothing Load
		{
			// Constant Low Background Load 50 RPS
			rpsFunc: constant(50),
			task: func(ctx context.Context, c servicepb.ComplexServiceClient) {
				_, _ = c.DoNothing(ctx, &importmepb.NothingIn{})
			},
		},
	}

	for _, s := range scenarios {
		wg.Go(func() { runLoad(ctx, c, s.rpsFunc, s.task) })
	}

	log.Printf("Load generators started. Press Ctrl+C to stop.")

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	log.Printf("Shutting down...")
	cancel()
	wg.Wait()
}

func runLoad(ctx context.Context, c servicepb.ComplexServiceClient, rpsFunc func(float64) float64, task func(context.Context, servicepb.ComplexServiceClient)) {
	// Update rate 10 times a second
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	startTime := time.Now()
	var accumulator float64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			elapsed := now.Sub(startTime).Seconds()
			rate := rpsFunc(elapsed)

			// Add requests for this 100ms interval
			// rate is req/sec. interval is 0.1 sec.
			accumulator += rate * 0.1

			count := int(accumulator)
			accumulator -= float64(count)

			if count > 0 {
				for i := 0; i < count; i++ {
					go task(ctx, c)
				}
			}
		}
	}
}

func sineWave(base, amp float64, period time.Duration, offset time.Duration) func(float64) float64 {
	freq := 1.0 / period.Seconds()
	return func(t float64) float64 {
		val := base + amp*math.Sin(2*math.Pi*freq*(t+offset.Seconds()))
		if val < 0 {
			return 0
		}
		return val
	}
}

func sawtoothWave(base, amp float64, period time.Duration, offset time.Duration) func(float64) float64 {
	freq := 1.0 / period.Seconds()
	return func(t float64) float64 {
		// Sawtooth: 0 to 1: ((t + offset) * freq) % 1
		x := (t + offset.Seconds()) * freq
		fraction := x - math.Floor(x)
		val := base + amp*fraction
		if val < 0 {
			return 0
		}
		return val
	}
}

func constant(rate float64) func(float64) float64 {
	return func(float64) float64 {
		return rate
	}
}

func getModelFabric(data string) func(context.Context, servicepb.ComplexServiceClient) {
	return func(ctx context.Context, c servicepb.ComplexServiceClient) {
		_, _ = c.GetModel(ctx, &modelspb.ServiceModel{
			Data: &importmepb.ImportedData{Value: data},
		})
	}
}

func getOldModelFabric(data string) func(context.Context, servicepb.ComplexServiceClient) {
	return func(ctx context.Context, c servicepb.ComplexServiceClient) {
		_, _ = c.GetOldModel(ctx, &deprecatedmodelspb.ServiceModel{
			Data: &importmepb.ImportedData{Value: data},
		})
	}
}
