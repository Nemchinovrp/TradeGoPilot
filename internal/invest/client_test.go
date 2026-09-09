package invest

import (
	"context"
	"net"
	"testing"
	"time"

	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type accountServer struct {
	pb.UnimplementedUsersServiceServer
	pb.UnimplementedSandboxServiceServer
}

func (accountServer) GetAccounts(ctx context.Context, _ *pb.GetAccountsRequest) (*pb.GetAccountsResponse, error) {
	grpc.SetHeader(ctx, metadata.Pairs("x-tracking-id", "production-id"))
	return &pb.GetAccountsResponse{Accounts: []*pb.Account{{Id: "production"}}}, nil
}
func (accountServer) GetSandboxAccounts(ctx context.Context, _ *pb.GetAccountsRequest) (*pb.GetAccountsResponse, error) {
	grpc.SetTrailer(ctx, metadata.Pairs("x-tracking-id", "sandbox-id"))
	return &pb.GetAccountsResponse{Accounts: []*pb.Account{{Id: "sandbox"}}}, nil
}
func TestAccountsRoutingAndCancellation(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterUsersServiceServer(server, accountServer{})
	pb.RegisterSandboxServiceServer(server, accountServer{})
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	for _, environment := range []string{"sandbox", "production"} {
		t.Run(environment, func(t *testing.T) {
			client := &Client{conn: conn, config: Config{Environment: environment, Timeout: time.Second}}
			response, md, err := client.Accounts(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(response.Accounts) != 1 || response.Accounts[0].Id != environment {
				t.Fatalf("wrong route: %v", response)
			}
			if ids := md.Get("x-tracking-id"); len(ids) != 1 || ids[0] != environment+"-id" {
				t.Fatalf("missing metadata: %v", md)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, _, err = client.Accounts(ctx)
			if status.Code(err) != codes.Canceled {
				t.Fatalf("expected cancellation, got %v", err)
			}
		})
	}
}
func TestConfigFromEnv(t *testing.T) {
	t.Setenv("INVEST_TOKEN", "test-token")
	t.Setenv("INVEST_ENV", "")
	t.Setenv("INVEST_APP_NAME", "")
	t.Setenv("INVEST_TIMEOUT", "")
	c, err := ConfigFromEnv()
	if err != nil || c.Environment != "sandbox" || c.Timeout != 10*time.Second {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"INVEST_TOKEN", ""}, {"INVEST_ENV", "prod"}, {"INVEST_TIMEOUT", "0s"}, {"INVEST_TIMEOUT", "-1s"}, {"INVEST_TIMEOUT", "bad"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
func TestAuthRequiresTLS(t *testing.T) {
	a := auth{token: "test-token", appName: "test-app"}
	md, err := a.GetRequestMetadata(context.Background())
	if err != nil || md["authorization"] != "Bearer test-token" || md["x-app-name"] != "test-app" || !a.RequireTransportSecurity() {
		t.Fatal("invalid credentials")
	}
}
