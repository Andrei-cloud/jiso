// manager_spec_drift_test.go pins the UAT round-10 flake: changing the
// manager's spec while a connection is online must rebuild the moov
// connection, because its reader unpacks inbound with the spec it was
// dialled with. A drifted connection silently dropped every response
// (moov skips UnpackErrors) and the sender timed out next to a server
// that had matched and answered.
package connection

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/suite"

	"jiso/internal/utils"
)

type SpecDriftSuite struct {
	suite.Suite

	visa *iso8583.MessageSpec
	flex *iso8583.MessageSpec
}

func TestSpecDriftSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(SpecDriftSuite))
}

// echoServer answers every visa 0100 with a visa 0110 echoing DE11.
func (s *SpecDriftSuite) echoServer() net.Listener {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)

	serve := func(c net.Conn) {
		defer func() { _ = c.Close() }()
		for {
			lh := make([]byte, 2)
			if _, err := io.ReadFull(c, lh); err != nil {
				return
			}
			body := make([]byte, binary.BigEndian.Uint16(lh))
			if _, err := io.ReadFull(c, body); err != nil {
				return
			}
			req := iso8583.NewMessage(s.visa)
			if err := req.Unpack(body); err != nil {
				continue
			}
			resp := iso8583.NewMessage(s.visa)
			resp.MTI("0110")
			if f := req.GetField(11); f != nil {
				v, _ := f.String()
				_ = resp.Field(11, v)
			}
			_ = resp.Field(39, "00")
			for _, id := range []int{3, 4, 22, 25, 32, 37, 41, 42} {
				if f := req.GetField(id); f != nil {
					v, _ := f.String()
					_ = resp.Field(id, v)
				}
			}
			_ = resp.Field(38, "112233")
			packed, err := resp.Pack()
			if err != nil {
				continue
			}
			hdr := make([]byte, 2)
			binary.BigEndian.PutUint16(hdr, uint16(len(packed)))
			_, _ = c.Write(append(hdr, packed...))
		}
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(c)
		}
	}()

	return ln
}

func (s *SpecDriftSuite) newManager(ln net.Listener, spec *iso8583.MessageSpec) *Manager {
	_, port, err := net.SplitHostPort(ln.Addr().String())
	s.Require().NoError(err)

	return NewManager("127.0.0.1", port, spec, false, 0, 3*time.Second, 5*time.Second, nil)
}

func (s *SpecDriftSuite) request() *iso8583.Message {
	m := iso8583.NewMessage(s.visa)
	m.MTI("0100")
	s.Require().NoError(m.Field(3, "000000"))
	s.Require().NoError(m.Field(22, "1000"))
	s.Require().NoError(m.Field(25, "59"))
	s.Require().NoError(m.Field(11, "424242"))

	return m
}

func (s *SpecDriftSuite) SetupTest() {
	var err error
	if s.visa, err = utils.CreateSpecFromFile("../../specs/visa.json"); err != nil {
		s.T().Fatal(err)
	}
	if s.flex, err = utils.CreateSpecFromFile("../../specs/flex.json"); err != nil {
		s.T().Fatal(err)
	}
}

// TestSetSpecWhileOnline: dial under flex, flip the spec to visa while
// online (no explicit Connect), then send a visa message: the response
// must arrive. Before the fix the reader stayed on flex, skipped the
// response as an UnpackError, and the send timed out.
func (s *SpecDriftSuite) TestSetSpecWhileOnline() {
	ln := s.echoServer()
	defer func() { _ = ln.Close() }()

	mgr := s.newManager(ln, s.flex)
	s.dial(mgr)
	defer func() { _ = mgr.Close() }()

	mgr.SetSpec(s.visa) // the drift entry point itself

	s.sendAndExpect(mgr)
}

// TestAdoptSpecForDriftAfterSetSpec mirrors the §2 's' sequence: a
// connection dialled before the visa file was loaded, spec changed via
// SetSpec, then the composed visa message sent through SendAsync.
func (s *SpecDriftSuite) TestAdoptSpecForDriftAfterSetSpec() {
	ln := s.echoServer()
	defer func() { _ = ln.Close() }()

	mgr := s.newManager(ln, s.flex)
	s.dial(mgr)
	defer func() { _ = mgr.Close() }()

	mgr.SetSpec(s.visa)

	ch, err := mgr.SendAsync(s.request(), "tx")
	s.Require().NoError(err)

	select {
	case resp := <-ch:
		s.Require().NotNil(resp)
		mti, _ := resp.GetMTI()
		s.Require().Equal("0110", mti)
	case <-time.After(7 * time.Second):
		s.T().Fatal("SendAsync never delivered (spec drift dropped the response)")
	}
}

// TestSetSpecWhileOfflineDoesNotDial keeps the cheap path honest:
// SetSpec offline only stores.
func (s *SpecDriftSuite) TestSetSpecWhileOfflineDoesNotDial() {
	ln := s.echoServer()
	defer func() { _ = ln.Close() }()

	mgr := s.newManager(ln, s.flex)
	mgr.SetSpec(s.visa)

	s.Require().False(mgr.IsConnected())
	s.Require().Equal(s.visa, mgr.GetSpec())
}

func (s *SpecDriftSuite) dial(mgr *Manager) {
	s.Require().NoError(mgr.Connect(false, utils.NewBinary2BytesAdapter()))
}

func (s *SpecDriftSuite) sendAndExpect(mgr *Manager) {
	msg := s.request()

	resp, err := mgr.Send(msg)
	if errors.Is(err, io.EOF) {
		s.T().Fatal(err)
	}
	s.Require().NoError(err)
	mti, _ := resp.GetMTI()
	s.Require().Equal("0110", mti)
}
