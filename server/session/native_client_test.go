package session

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player/camera"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type nativeOutputConn struct {
	Conn
	writes chan packet.Packet
	fail   error
}

func (c *nativeOutputConn) WritePacket(pk packet.Packet) error {
	if c.fail != nil {
		return c.fail
	}
	c.writes <- pk
	return nil
}
func (*nativeOutputConn) Close() error { return nil }
func nativeOutputSession(capacity int) (*Session, *nativeOutputConn) {
	c := &nativeOutputConn{writes: make(chan packet.Packet, 32)}
	s := &Session{conn: c, packets: make(chan packet.Packet, capacity), closeBackground: make(chan struct{})}
	s.registerHandlers()
	return s, c
}

func TestNativeDeliveryOrderingCancellationAndClose(t *testing.T) {
	s, c := nativeOutputSession(8)
	s.RegisterCamera()
	s.SetCamera(camera.View{})
	s.writePacket(&packet.MovePlayer{Mode: packet.MoveModeTeleport})
	clear := s.ClearCamera()
	cancelled := s.ClearTitle()
	if !cancelled.Cancel() {
		t.Fatal("pending delivery could not be cancelled")
	}
	go s.writeOutput()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := clear.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint32{packet.IDCameraPresets, packet.IDCameraInstruction, packet.IDMovePlayer, packet.IDCameraInstruction} {
		select {
		case pk := <-c.writes:
			if pk.ID() != id {
				t.Fatalf("got %T, want ID %d", pk, id)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	s.CloseConnection()
	if err := cancelled.Err(); !errors.Is(err, ErrDeliveryCancelled) {
		t.Fatal(err)
	}
	if err := s.ClearCamera().Wait(ctx); !errors.Is(err, net.ErrClosed) {
		t.Fatal("closed connection accepted camera", err)
	}
	select {
	case pk := <-c.writes:
		t.Fatalf("cancelled output was written: %T", pk)
	default:
	}
}

func TestNativeDeliveryBackpressureAndWriteFailure(t *testing.T) {
	s, c := nativeOutputSession(1)
	pending := s.ClearCamera()
	if err := s.ClearTitle().Err(); !errors.Is(err, ErrOutputFull) {
		t.Fatal(err)
	}
	c.fail = io.ErrUnexpectedEOF
	go s.writeOutput()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := pending.Wait(ctx); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	select {
	case <-s.Connection().Done():
	case <-ctx.Done():
		t.Fatal("write failure did not end connection")
	}
	end, ok := s.Connection().End()
	if !ok || end.Kind != ConnectionEndTransport || !errors.Is(end.Err, io.ErrUnexpectedEOF) {
		t.Fatal(end)
	}
	s2, _ := nativeOutputSession(2)
	queued := s2.ClearCamera()
	s2.CloseConnection()
	if err := queued.Wait(ctx); !errors.Is(err, net.ErrClosed) {
		t.Fatal("shutdown stranded pending delivery", err)
	}
}

type nativeSettingsForm struct {
	submitted int
	data      []byte
}

func (*nativeSettingsForm) MarshalJSON() ([]byte, error) {
	return []byte(`{"type":"custom_form","title":"Settings","content":[]}`), nil
}
func (f *nativeSettingsForm) SubmitJSON(data []byte, _ form.Submitter, _ *world.Tx) error {
	f.submitted++
	f.data = data
	return nil
}

func TestNativeServerSettingsIDsAndStaleResponses(t *testing.T) {
	s, _ := nativeOutputSession(8)
	f := &nativeSettingsForm{}
	s.SendServerSettings(f)
	pk := (<-s.packets).(deliveryPacket).Packet.(*packet.ServerSettingsResponse)
	if pk.FormID&(1<<31) == 0 {
		t.Fatal("settings overlaps ordinary forms")
	}
	if err := s.SendServerSettings(f).Err(); !errors.Is(err, ErrServerSettingsOpen) {
		t.Fatal(err)
	}
	h := s.handlers[packet.IDModalFormResponse]
	response := &packet.ModalFormResponse{FormID: pk.FormID, ResponseData: protocol.Option([]byte(`[]`))}
	if err := h.Handle(response, s, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(response, s, nil, nil); err != nil {
		t.Fatal("stale settings response was rejected", err)
	}
	if f.submitted != 1 {
		t.Fatal("settings submitted more than once", f.submitted)
	}
	s.SendServerSettings(f)
	newID := (<-s.packets).(deliveryPacket).Packet.(*packet.ServerSettingsResponse).FormID
	if newID == pk.FormID {
		t.Fatal("settings reused stale ID")
	}
}

func TestFormImageRefreshIsCancelledOnlyByItsOwnResponse(t *testing.T) {
	s, _ := nativeOutputSession(8)
	s.formImages.formID, s.formImages.generation, s.formImages.pending = 7, 3, 99
	s.cancelFormImagesFor(6)
	if s.formImages.generation != 3 || s.formImages.pending != 99 {
		t.Fatal("old form cancelled current image refresh")
	}
	s.cancelFormImagesFor(7)
	if s.formImages.generation != 4 || s.formImages.pending != 0 {
		t.Fatal("active response left refresh running")
	}
}
