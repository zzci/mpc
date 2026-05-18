package mpc

import (
	"context"
	"fmt"

	"github.com/bnb-chain/tss-lib/v3/tss"
)

// runProtocol drives an in-process tss-lib protocol round to completion using
// only Go channels as the transport — there is no network, no relay, and no
// coordinator. Every party runs in its own goroutine and exchanges
// tss.Message values through outCh; the loop fans them out to the destination
// parties (broadcast or point-to-point) exactly as a real transport would,
// then collects one end-result per party from endCh.
//
// It is generic over the protocol end type E (e.g. *keygen.LocalPartySaveData)
// so the same wiring can back keygen now and signing/resharing simulation and
// the multi-process E2E carrier (MPC-009) later.
//
// The function returns when every party has produced a result, or early on the
// first protocol error or context cancellation.
func runProtocol[E any](
	ctx context.Context,
	parties []tss.Party,
	outCh <-chan tss.Message,
	endCh <-chan E,
) ([]E, error) {
	n := len(parties)
	errCh := make(chan *tss.Error, n)

	for _, p := range parties {
		go func(p tss.Party) {
			if err := p.Start(); err != nil {
				errCh <- err
			}
		}(p)
	}

	results := make([]E, 0, n)
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("protocol cancelled: %w", ctx.Err())
		case err := <-errCh:
			return nil, fmt.Errorf("tss protocol error: %w", err)
		case msg := <-outCh:
			if err := routeMessage(parties, msg, errCh); err != nil {
				return nil, err
			}
		case res := <-endCh:
			results = append(results, res)
			if len(results) == n {
				return results, nil
			}
		}
	}
}

// routeMessage delivers one outbound message to its destination party or
// parties, mirroring the dispatch logic of the tss-lib reference test loop.
func routeMessage(parties []tss.Party, msg tss.Message, errCh chan<- *tss.Error) error {
	dest := msg.GetTo()
	if dest == nil { // broadcast
		for _, p := range parties {
			if p.PartyID().Index == msg.GetFrom().Index {
				continue
			}
			go updateParty(p, msg, errCh)
		}
		return nil
	}
	if dest[0].Index == msg.GetFrom().Index {
		return fmt.Errorf("party %d tried to send a message to itself", dest[0].Index)
	}
	go updateParty(parties[dest[0].Index], msg, errCh)
	return nil
}

// updateParty applies a message to a party after a wire-bytes round trip, so
// the in-process simulation exercises the same (de)serialization path a real
// transport would. This intentionally reproduces tss-lib's SharedPartyUpdater
// rather than importing the upstream test helper into non-test code.
func updateParty(party tss.Party, msg tss.Message, errCh chan<- *tss.Error) {
	if party.PartyID() == msg.GetFrom() {
		return
	}
	bz, _, err := msg.WireBytes()
	if err != nil {
		errCh <- party.WrapError(err)
		return
	}
	parsed, err := tss.ParseWireMessage(bz, msg.GetFrom(), msg.IsBroadcast())
	if err != nil {
		errCh <- party.WrapError(err)
		return
	}
	if _, uerr := party.Update(parsed); uerr != nil {
		errCh <- uerr
	}
}
