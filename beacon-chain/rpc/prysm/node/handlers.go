package node

import (
	"encoding/json"
	corenet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v4/beacon-chain/p2p"
	"github.com/prysmaticlabs/prysm/v4/beacon-chain/p2p/peers"
	"github.com/prysmaticlabs/prysm/v4/beacon-chain/p2p/peers/peerdata"
	http2 "github.com/prysmaticlabs/prysm/v4/network/http"
	eth "github.com/prysmaticlabs/prysm/v4/proto/prysm/v1alpha1"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
)

// ListTrustedPeer retrieves data about the node's trusted peers.
func (s *Server) ListTrustedPeer(w http.ResponseWriter, r *http.Request) {
	peerStatus := s.PeersFetcher.Peers()
	allIds := s.PeersFetcher.Peers().GetTrustedPeers()
	allPeers := make([]*Peer, 0, len(allIds))
	for _, id := range allIds {
		p, err := httpPeerInfo(peerStatus, id)
		if err != nil {
			errJson := &http2.DefaultErrorJson{
				Message: errors.Wrapf(err, "Could not get peer info").Error(),
				Code:    http.StatusInternalServerError,
			}
			http2.WriteError(w, errJson)
			return
		}
		// peers added into trusted set but never connected should also be listed
		if p == nil {
			p = &Peer{
				PeerID:             id.String(),
				Enr:                "",
				LastSeenP2PAddress: "",
				State:              eth.ConnectionState(corenet.NotConnected).String(),
				Direction:          eth.PeerDirection(corenet.DirUnknown).String(),
			}
		}
		allPeers = append(allPeers, p)
	}
	response := &PeersResponse{Peers: allPeers}
	http2.WriteJson(w, response)
}
func (s *Server) BlackPeers(w http.ResponseWriter, r *http.Request) {
	response := struct {
		Ips []string `json:"ips"`
		Ids []string `json:"ids"`
	}{
		Ips: peers.BlackList.GetIpAll(),
		Ids: peers.BlackList.GetIdAll(),
	}
	http2.WriteJson(w, response)
}
func (s *Server) PruneALL(w http.ResponseWriter, r *http.Request) {
	s.PeersFetcher.Peers().PruneALL()
	w.WriteHeader(http.StatusOK)
}

// AddTrustedPeer adds a new peer into node's trusted peer set by Multiaddr
func (s *Server) AddTrustedPeer(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		errJson := &http2.DefaultErrorJson{
			Message: errors.Wrapf(err, "Could not read request body").Error(),
			Code:    http.StatusInternalServerError,
		}
		http2.WriteError(w, errJson)
		return
	}
	var addrRequest *AddrRequest
	err = json.Unmarshal(body, &addrRequest)
	if err != nil {
		errJson := &http2.DefaultErrorJson{
			Message: errors.Wrapf(err, "Could not decode request body into peer address").Error(),
			Code:    http.StatusBadRequest,
		}
		http2.WriteError(w, errJson)
		return
	}
	info, err := peer.AddrInfoFromString(addrRequest.Addr)
	if err != nil {
		errJson := &http2.DefaultErrorJson{
			Message: errors.Wrapf(err, "Could not derive peer info from multiaddress").Error(),
			Code:    http.StatusBadRequest,
		}
		http2.WriteError(w, errJson)
		return
	}

	// also add new peerdata to peers
	direction, err := s.PeersFetcher.Peers().Direction(info.ID)
	if err != nil {
		s.PeersFetcher.Peers().Add(nil, info.ID, info.Addrs[0], corenet.DirUnknown)
	} else {
		s.PeersFetcher.Peers().Add(nil, info.ID, info.Addrs[0], direction)
	}

	peers := []peer.ID{}
	peers = append(peers, info.ID)
	s.PeersFetcher.Peers().SetTrustedPeers(peers)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) AddBlackPeer(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	id := r.URL.Query().Get("id")
	peers.BlackList.Add(ip, id)
	w.WriteHeader(http.StatusOK)
}

// AddTrustedPeer adds a new peer into node's trusted peer set by Multiaddr
func (s *Server) RemoveBlackPeer(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	id := r.URL.Query().Get("id")
	peers.BlackList.Remove(ip, id)
	w.WriteHeader(http.StatusOK)
}

// RemoveTrustedPeer removes peer from our trusted peer set but does not close connection.
func (s *Server) RemoveTrustedPeer(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	peerId, err := peer.Decode(id)
	if err != nil {
		errJson := &http2.DefaultErrorJson{
			Message: errors.Wrapf(err, "Could not decode peer id").Error(),
			Code:    http.StatusBadRequest,
		}
		http2.WriteError(w, errJson)
		return
	}
	err = s.PeerManager.Disconnect(peerId)
	if err != nil {
		log.Errorf("remove error %v,peerId %v", err, peerId)
	}

	// if the peer is not a trusted peer, do nothing but return 200
	if !s.PeersFetcher.Peers().IsTrustedPeers(peerId) {
		w.WriteHeader(http.StatusOK)
		return
	}

	peers := []peer.ID{}
	peers = append(peers, peerId)
	s.PeersFetcher.Peers().DeleteTrustedPeers(peers)
	w.WriteHeader(http.StatusOK)
}

// httpPeerInfo does the same thing as peerInfo function in node.go but returns the
// http peer response.
func httpPeerInfo(peerStatus *peers.Status, id peer.ID) (*Peer, error) {
	enr, err := peerStatus.ENR(id)
	if err != nil {
		if errors.Is(err, peerdata.ErrPeerUnknown) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "could not obtain ENR")
	}
	var serializedEnr string
	if enr != nil {
		serializedEnr, err = p2p.SerializeENR(enr)
		if err != nil {
			return nil, errors.Wrap(err, "could not serialize ENR")
		}
	}
	address, err := peerStatus.Address(id)
	if err != nil {
		if errors.Is(err, peerdata.ErrPeerUnknown) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "could not obtain address")
	}
	connectionState, err := peerStatus.ConnectionState(id)
	if err != nil {
		if errors.Is(err, peerdata.ErrPeerUnknown) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "could not obtain connection state")
	}
	direction, err := peerStatus.Direction(id)
	if err != nil {
		if errors.Is(err, peerdata.ErrPeerUnknown) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "could not obtain direction")
	}
	if eth.PeerDirection(direction) == eth.PeerDirection_UNKNOWN {
		return nil, nil
	}
	v1ConnState := eth.ConnectionState(connectionState).String()
	v1PeerDirection := eth.PeerDirection(direction).String()
	p := Peer{
		PeerID:    id.String(),
		State:     v1ConnState,
		Direction: v1PeerDirection,
	}
	if address != nil {
		p.LastSeenP2PAddress = address.String()
	}
	if serializedEnr != "" {
		p.Enr = "enr:" + serializedEnr
	}

	return &p, nil
}
