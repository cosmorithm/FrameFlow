package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cosmorithm/FrameFlow/internal/interaction"
	pb "github.com/cosmorithm/FrameFlow/proto/interaction/v1"
	"github.com/go-sql-driver/mysql"
	"google.golang.org/grpc"
)

// Limit database work even when an internal client omits its own deadline.
func boundRPC(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return handler(ctx, req)
}

func run() error {
	dsn := os.Getenv("INTERACTION_DSN")
	if dsn == "" {
		return fmt.Errorf("set INTERACTION_DSN to the MySQL DSN for the interaction schema")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("invalid INTERACTION_DSN")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = make(map[string]string)
	}
	cfg.Params["time_zone"] = "'+00:00'"
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return fmt.Errorf("create MySQL connector: %w", err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	db.SetMaxOpenConns(24)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(3 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = db.PingContext(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("connect to interaction database: %w", err)
	}

	addr := os.Getenv("INTERACTION_LISTEN")
	if addr == "" {
		addr = "127.0.0.1:9091"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := grpc.NewServer(grpc.MaxRecvMsgSize(1<<20), grpc.UnaryInterceptor(boundRPC))
	pb.RegisterInteractionServiceServer(server, interaction.New(db))
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-signalCtx.Done()
		force := time.AfterFunc(3*time.Second, server.Stop)
		server.GracefulStop()
		force.Stop()
	}()
	log.Printf("interaction gRPC listening at %s", listener.Addr())
	return server.Serve(listener)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
