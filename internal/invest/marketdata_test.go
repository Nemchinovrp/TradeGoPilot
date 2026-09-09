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
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type marketServer struct {
	pb.UnimplementedMarketDataStreamServiceServer
	pb.UnimplementedInstrumentsServiceServer
	reject bool
}

func (s marketServer) GetInstrumentBy(_ context.Context, r *pb.InstrumentRequest) (*pb.InstrumentResponse, error) {
	if r.Id != "SBER" || r.ClassCode != "TQBR" || r.IdType != pb.InstrumentIdType_INSTRUMENT_ID_TYPE_TICKER {
		return nil, status.Error(codes.InvalidArgument, "wrong instrument request")
	}
	return &pb.InstrumentResponse{Instrument: &pb.Instrument{Ticker: "SBER", ClassCode: "TQBR", Uid: "sber-uid"}}, nil
}

func (s marketServer) MarketDataStream(stream pb.MarketDataStreamService_MarketDataStreamServer) error {
	b, err := stream.Recv()
	if err != nil {
		return err
	}
	tr, err := stream.Recv()
	if err != nil {
		return err
	}
	bs, ts := b.GetSubscribeOrderBookRequest(), tr.GetSubscribeTradesRequest()
	if bs.GetSubscriptionAction() != pb.SubscriptionAction_SUBSCRIPTION_ACTION_SUBSCRIBE || ts.GetSubscriptionAction() != pb.SubscriptionAction_SUBSCRIPTION_ACTION_SUBSCRIBE || len(bs.GetInstruments()) != 1 || len(ts.GetInstruments()) != 1 {
		return status.Error(codes.InvalidArgument, "wrong subscriptions")
	}
	if bs.Instruments[0].InstrumentId != "sber-uid" || bs.Instruments[0].Depth != 10 || ts.Instruments[0].InstrumentId != "sber-uid" {
		return status.Error(codes.InvalidArgument, "wrong subscription instrument")
	}
	subStatus := pb.SubscriptionStatus_SUBSCRIPTION_STATUS_SUCCESS
	if s.reject {
		subStatus = pb.SubscriptionStatus_SUBSCRIPTION_STATUS_INSTRUMENT_NOT_FOUND
	}
	if err := stream.Send(&pb.MarketDataResponse{Payload: &pb.MarketDataResponse_SubscribeOrderBookResponse{SubscribeOrderBookResponse: &pb.SubscribeOrderBookResponse{OrderBookSubscriptions: []*pb.OrderBookSubscription{{InstrumentUid: "sber-uid", Depth: 10, SubscriptionStatus: subStatus}}}}}); err != nil {
		return err
	}
	if err := stream.Send(&pb.MarketDataResponse{Payload: &pb.MarketDataResponse_SubscribeTradesResponse{SubscribeTradesResponse: &pb.SubscribeTradesResponse{TradeSubscriptions: []*pb.TradeSubscription{{InstrumentUid: "sber-uid", SubscriptionStatus: subStatus}}}}}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return stream.Context().Err()
}

func marketClient(t *testing.T, reject bool) *Client {
	t.Helper()
	l := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	pb.RegisterMarketDataStreamServiceServer(s, marketServer{reject: reject})
	pb.RegisterInstrumentsServiceServer(s, marketServer{})
	go s.Serve(l)
	t.Cleanup(s.Stop)
	conn, err := grpc.NewClient("passthrough:///market", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return l.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &Client{conn: conn, config: Config{Timeout: 100 * time.Millisecond}}
}

func TestMarketStreamOutlivesUnaryTimeout(t *testing.T) {
	c := marketClient(t, false)
	i, err := c.Instrument(context.Background(), "SBER", "TQBR")
	if err != nil || i.GetUid() != "sber-uid" {
		t.Fatalf("instrument: %v %v", i, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	ack := false
	err = c.WatchMarket(ctx, i.Uid, func(*pb.MarketDataResponse) error { ack = true; return nil })
	if !ack || ctx.Err() == nil || status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("stream ended before caller deadline: ack=%v, err=%v", ack, err)
	}
}

func TestMarketSubscriptionRejected(t *testing.T) {
	c := marketClient(t, true)
	called := false
	err := c.WatchMarket(context.Background(), "sber-uid", func(*pb.MarketDataResponse) error { called = true; return nil })
	if status.Code(err) != codes.InvalidArgument || called {
		t.Fatalf("rejected subscription delivered data: %v", err)
	}
}
