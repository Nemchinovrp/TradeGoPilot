package invest

import (
	"context"
	"fmt"
	"time"

	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Instrument resolves an exact ticker/class pair instead of guessing a FIGI.
func (c *Client) Instrument(ctx context.Context, ticker, class string) (*pb.Instrument, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	r, err := pb.NewInstrumentsServiceClient(c.conn).GetInstrumentBy(ctx, &pb.InstrumentRequest{
		IdType: pb.InstrumentIdType_INSTRUMENT_ID_TYPE_TICKER, Id: ticker, ClassCode: class,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve instrument: %w", err)
	}
	i := r.GetInstrument()
	if i.GetTicker() != ticker || i.GetClassCode() != class || i.GetUid() == "" {
		return nil, fmt.Errorf("unexpected instrument for %s/%s", ticker, class)
	}
	return i, nil
}

// WatchMarket subscribes to a depth-10 book and trades. The unary timeout only
// limits subscription setup; the caller's context controls the stream lifetime.
// The handler is called serially, after both subscriptions are acknowledged.
func (c *Client) WatchMarket(ctx context.Context, uid string, handler func(*pb.MarketDataResponse) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	setup := time.AfterFunc(c.config.Timeout, cancel)
	defer setup.Stop()
	// Default API pings arrive every 120 seconds; a silent stream must reconnect.
	idle := time.AfterFunc(150*time.Second, cancel)
	defer idle.Stop()
	stream, err := pb.NewMarketDataStreamServiceClient(c.conn).MarketDataStream(ctx)
	if err != nil {
		return err
	}
	defer stream.CloseSend()
	requests := []*pb.MarketDataRequest{
		{Payload: &pb.MarketDataRequest_SubscribeOrderBookRequest{SubscribeOrderBookRequest: &pb.SubscribeOrderBookRequest{
			SubscriptionAction: pb.SubscriptionAction_SUBSCRIPTION_ACTION_SUBSCRIBE,
			Instruments:        []*pb.OrderBookInstrument{{InstrumentId: uid, Depth: 10}},
		}}},
		{Payload: &pb.MarketDataRequest_SubscribeTradesRequest{SubscribeTradesRequest: &pb.SubscribeTradesRequest{
			SubscriptionAction: pb.SubscriptionAction_SUBSCRIPTION_ACTION_SUBSCRIBE,
			Instruments:        []*pb.TradeInstrument{{InstrumentId: uid}},
		}}},
	}
	for _, request := range requests {
		if err := stream.Send(request); err != nil {
			return err
		}
	}
	bookOK, tradesOK := false, false
	for {
		r, err := stream.Recv()
		if err != nil {
			if (!bookOK || !tradesOK) && ctx.Err() != nil {
				return fmt.Errorf("market subscriptions not ready: %w", err)
			}
			return err
		}
		idle.Reset(150 * time.Second)
		for _, s := range r.GetSubscribeOrderBookResponse().GetOrderBookSubscriptions() {
			if s.GetSubscriptionStatus() != pb.SubscriptionStatus_SUBSCRIPTION_STATUS_SUCCESS {
				return status.Errorf(codes.InvalidArgument, "order book subscription: %s", s.GetSubscriptionStatus())
			}
			bookOK = s.GetInstrumentUid() == uid && s.GetDepth() == 10
		}
		for _, s := range r.GetSubscribeTradesResponse().GetTradeSubscriptions() {
			if s.GetSubscriptionStatus() != pb.SubscriptionStatus_SUBSCRIPTION_STATUS_SUCCESS {
				return status.Errorf(codes.InvalidArgument, "trades subscription: %s", s.GetSubscriptionStatus())
			}
			tradesOK = s.GetInstrumentUid() == uid
		}
		if bookOK && tradesOK {
			setup.Stop()
			if err := handler(r); err != nil {
				return err
			}
		}
	}
}
