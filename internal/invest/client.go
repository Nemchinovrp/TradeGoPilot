package invest

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"strings"
	"time"

	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type Config struct {
	Token       string
	Environment string
	AppName     string
	Timeout     time.Duration
}

func ConfigFromEnv() (Config, error) {
	c := Config{Token: strings.TrimSpace(os.Getenv("INVEST_TOKEN")), Environment: os.Getenv("INVEST_ENV"), AppName: os.Getenv("INVEST_APP_NAME"), Timeout: 10 * time.Second}
	if c.Environment == "" {
		c.Environment = "sandbox"
	}
	if c.AppName == "" {
		c.AppName = "roman.TradeGoPilot"
	}
	if v := os.Getenv("INVEST_TIMEOUT"); v != "" {
		var err error
		c.Timeout, err = time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("INVEST_TIMEOUT must be a duration such as 10s")
		}
	}
	return c, c.validate()
}
func (c Config) validate() error {
	if c.Token == "" {
		return fmt.Errorf("INVEST_TOKEN is required")
	}
	if c.Environment != "sandbox" && c.Environment != "production" {
		return fmt.Errorf("INVEST_ENV must be sandbox or production")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("INVEST_TIMEOUT must be positive")
	}
	return nil
}

type auth struct{ token, appName string }

func (a auth) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + a.token, "x-app-name": a.appName}, nil
}
func (auth) RequireTransportSecurity() bool { return true }

type Client struct {
	conn   *grpc.ClientConn
	config Config
}

// New creates a TLS client. The first RPC establishes the connection.
func New(c Config) (*Client, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	endpoint := "sandbox-invest-public-api.tinkoff.ru:443"
	if c.Environment == "production" {
		endpoint = "invest-public-api.tinkoff.ru:443"
	}
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})),
		grpc.WithPerRPCCredentials(auth{c.Token, c.AppName}),
	)
	if err != nil {
		return nil, fmt.Errorf("create invest client: %w", err)
	}
	return &Client{conn: conn, config: c}, nil
}
func (c *Client) Close() error { return c.conn.Close() }

// Accounts reads existing accounts without creating sandbox accounts.
// Returned metadata includes headers and trailers even on RPC errors.
func (c *Client) Accounts(ctx context.Context) (*pb.GetAccountsResponse, metadata.MD, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	var header, trailer metadata.MD
	opts := []grpc.CallOption{grpc.Header(&header), grpc.Trailer(&trailer)}
	var response *pb.GetAccountsResponse
	var err error
	if c.config.Environment == "sandbox" {
		response, err = pb.NewSandboxServiceClient(c.conn).GetSandboxAccounts(ctx, &pb.GetAccountsRequest{}, opts...)
	} else {
		response, err = pb.NewUsersServiceClient(c.conn).GetAccounts(ctx, &pb.GetAccountsRequest{}, opts...)
	}
	if err != nil {
		err = fmt.Errorf("get accounts: %w", err)
	}
	return response, metadata.Join(header, trailer), err
}
