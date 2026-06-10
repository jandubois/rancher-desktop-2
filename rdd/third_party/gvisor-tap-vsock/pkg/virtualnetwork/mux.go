package virtualnetwork

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"

	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/inetaf/tcpproxy"
	log "github.com/sirupsen/logrus"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
)

func (n *VirtualNetwork) ServicesMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/services/", http.StripPrefix("/services", n.servicesMux))
	mux.HandleFunc("/stats", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(statsAsJSON(n.networkSwitch.Sent, n.networkSwitch.Received, n.stack.Stats()))
	})
	mux.HandleFunc("/cam", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(n.networkSwitch.CAM())
	})
	mux.HandleFunc("/leases", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(n.ipPool.Leases())
	})
	// /neighbors exposes the netstack NUD neighbor cache for NIC 1 (the gap the
	// V6 investigation needed: /cam shows the switch layer, /stats shows ARP
	// counters, but neither shows the neighbor-entry STATE that decides whether a
	// dial emits an ARP, sends a SYN, or fast-fails EHOSTUNREACH).
	mux.HandleFunc("/neighbors", func(w http.ResponseWriter, _ *http.Request) {
		type neighbor struct {
			Addr     string `json:"addr"`
			LinkAddr string `json:"linkAddr"`
			State    string `json:"state"`
		}
		out := []neighbor{}
		if entries, err := n.stack.Neighbors(1, ipv4.ProtocolNumber); err == nil {
			for _, e := range entries {
				out = append(out, neighbor{e.Addr.String(), e.LinkAddr.String(), e.State.String()})
			}
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	// /nicinfo exposes NIC flags + assigned addresses + the route table, to test
	// the route/NIC-level hypothesis: a dial that fast-fails EHOSTUNREACH with no
	// ARP and no SYN is a route-resolution failure, which means either the NIC is
	// down/not-running or it lost the gateway address or the subnet route.
	mux.HandleFunc("/nicinfo", func(w http.ResponseWriter, _ *http.Request) {
		type nic struct {
			Name        string   `json:"name"`
			LinkAddr    string   `json:"linkAddr"`
			Up          bool     `json:"up"`
			Running     bool     `json:"running"`
			Promiscuous bool     `json:"promiscuous"`
			Addrs       []string `json:"addrs"`
		}
		type route struct {
			Destination string `json:"destination"`
			Gateway     string `json:"gateway"`
			NIC         uint32 `json:"nic"`
		}
		nics := map[string]nic{}
		for id, info := range n.stack.NICInfo() {
			addrs := []string{}
			for _, pa := range info.ProtocolAddresses {
				addrs = append(addrs, pa.AddressWithPrefix.String())
			}
			nics[strconv.Itoa(int(id))] = nic{
				info.Name, info.LinkAddress.String(),
				info.Flags.Up, info.Flags.Running, info.Flags.Promiscuous, addrs,
			}
		}
		routes := []route{}
		for _, r := range n.stack.GetRouteTable() {
			routes = append(routes, route{r.Destination.String(), r.Gateway.String(), uint32(r.NIC)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"nics": nics, "routes": routes})
	})
	mux.HandleFunc("/tunnel", func(w http.ResponseWriter, r *http.Request) {
		ip := r.URL.Query().Get("ip")
		if ip == "" {
			http.Error(w, "ip is mandatory", http.StatusInternalServerError)
			return
		}
		port, err := strconv.ParseUint(r.URL.Query().Get("port"), 10, 16)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		port16 := uint16(port)

		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
			return
		}

		conn, bufrw, err := hj.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer conn.Close()

		if err := bufrw.Flush(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if _, err := conn.Write([]byte(`OK`)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		remote := tcpproxy.DialProxy{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return gonet.DialContextTCP(ctx, n.stack, tcpip.FullAddress{
					NIC:  1,
					Addr: tcpip.AddrFrom4Slice(net.ParseIP(ip).To4()),
					Port: port16,
				}, ipv4.ProtocolNumber)
			},
			OnDialError: func(_ net.Conn, dstDialErr error) {
				log.Errorf("cannot dial: %v", dstDialErr)
			},
		}
		remote.HandleConn(conn)
	})
	return mux
}

func (n *VirtualNetwork) Mux() *http.ServeMux {
	mux := n.ServicesMux()
	mux.HandleFunc(types.ConnectPath, func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
			return
		}
		conn, bufrw, err := hj.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer conn.Close()

		if err := bufrw.Flush(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_ = n.networkSwitch.Accept(context.Background(), conn, n.configuration.Protocol)
	})
	return mux
}
