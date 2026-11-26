//go:build with_naive_outbound

package main

import (
	"encoding/json"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/naive"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/common/network"

	"github.com/stretchr/testify/require"
)

func TestNaiveSelf(t *testing.T) {
	caPem, certPem, keyPem := createSelfSignedCertificate(t, "example.org")
	caPemContent, err := os.ReadFile(caPem)
	require.NoError(t, err)
	startInstance(t, option.Options{
		Inbounds: []option.Inbound{
			{
				Type: C.TypeMixed,
				Tag:  "mixed-in",
				Options: &option.HTTPMixedInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
						ListenPort: clientPort,
					},
				},
			},
			{
				Type: C.TypeNaive,
				Tag:  "naive-in",
				Options: &option.NaiveInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
						ListenPort: serverPort,
					},
					Users: []auth.User{
						{
							Username: "sekai",
							Password: "password",
						},
					},
					Network: network.NetworkTCP,
					InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
						TLS: &option.InboundTLSOptions{
							Enabled:         true,
							ServerName:      "example.org",
							CertificatePath: certPem,
							KeyPath:         keyPem,
						},
					},
				},
			},
		},
		Outbounds: []option.Outbound{
			{
				Type: C.TypeDirect,
			},
			{
				Type: C.TypeNaive,
				Tag:  "naive-out",
				Options: &option.NaiveOutboundOptions{
					ServerOptions: option.ServerOptions{
						Server:     "127.0.0.1",
						ServerPort: serverPort,
					},
					Username: "sekai",
					Password: "password",
					OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
						TLS: &option.OutboundTLSOptions{
							Enabled:     true,
							ServerName:  "example.org",
							Certificate: []string{string(caPemContent)},
						},
					},
				},
			},
		},
		Route: &option.RouteOptions{
			Rules: []option.Rule{
				{
					Type: C.RuleTypeDefault,
					DefaultOptions: option.DefaultRule{
						RawDefaultRule: option.RawDefaultRule{
							Inbound: []string{"mixed-in"},
						},
						RuleAction: option.RuleAction{
							Action: C.RuleActionTypeRoute,
							RouteOptions: option.RouteActionOptions{
								Outbound: "naive-out",
							},
						},
					},
				},
			},
		},
	})
	testTCP(t, clientPort, testPort)
}

func TestNaiveSelfECH(t *testing.T) {
	caPem, certPem, keyPem := createSelfSignedCertificate(t, "example.org")
	caPemContent, err := os.ReadFile(caPem)
	require.NoError(t, err)
	echConfig, echKey := common.Must2(tls.ECHKeygenDefault("not.example.org"))
	instance := startInstance(t, option.Options{
		Inbounds: []option.Inbound{
			{
				Type: C.TypeMixed,
				Tag:  "mixed-in",
				Options: &option.HTTPMixedInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
						ListenPort: clientPort,
					},
				},
			},
			{
				Type: C.TypeNaive,
				Tag:  "naive-in",
				Options: &option.NaiveInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
						ListenPort: serverPort,
					},
					Users: []auth.User{
						{
							Username: "sekai",
							Password: "password",
						},
					},
					Network: network.NetworkTCP,
					InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
						TLS: &option.InboundTLSOptions{
							Enabled:         true,
							ServerName:      "example.org",
							CertificatePath: certPem,
							KeyPath:         keyPem,
							ECH: &option.InboundECHOptions{
								Enabled: true,
								Key:     []string{echKey},
							},
						},
					},
				},
			},
		},
		Outbounds: []option.Outbound{
			{
				Type: C.TypeDirect,
			},
			{
				Type: C.TypeNaive,
				Tag:  "naive-out",
				Options: &option.NaiveOutboundOptions{
					ServerOptions: option.ServerOptions{
						Server:     "127.0.0.1",
						ServerPort: serverPort,
					},
					Username: "sekai",
					Password: "password",
					OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
						TLS: &option.OutboundTLSOptions{
							Enabled:     true,
							ServerName:  "example.org",
							Certificate: []string{string(caPemContent)},
							ECH: &option.OutboundECHOptions{
								Enabled: true,
								Config:  []string{echConfig},
							},
						},
					},
				},
			},
		},
		Route: &option.RouteOptions{
			Rules: []option.Rule{
				{
					Type: C.RuleTypeDefault,
					DefaultOptions: option.DefaultRule{
						RawDefaultRule: option.RawDefaultRule{
							Inbound: []string{"mixed-in"},
						},
						RuleAction: option.RuleAction{
							Action: C.RuleActionTypeRoute,
							RouteOptions: option.RouteActionOptions{
								Outbound: "naive-out",
							},
						},
					},
				},
			},
		},
	})

	naiveOut, ok := instance.Outbound().Outbound("naive-out")
	require.True(t, ok)
	naiveOutbound := naiveOut.(*naive.Outbound)

	netLogPath := "/tmp/naive_ech_netlog.json"
	require.True(t, naiveOutbound.Client().Engine().StartNetLogToFile(netLogPath, true))
	defer naiveOutbound.Client().Engine().StopNetLog()

	testTCP(t, clientPort, testPort)

	naiveOutbound.Client().Engine().StopNetLog()

	logContent, err := os.ReadFile(netLogPath)
	require.NoError(t, err)
	logStr := string(logContent)

	require.True(t, strings.Contains(logStr, `"encrypted_client_hello":true`),
		"ECH should be accepted in TLS handshake. NetLog saved to: %s", netLogPath)
}

func TestNaiveSelfInsecureConcurrency(t *testing.T) {
	caPem, certPem, keyPem := createSelfSignedCertificate(t, "example.org")
	caPemContent, err := os.ReadFile(caPem)
	require.NoError(t, err)

	instance := startInstance(t, option.Options{
		Inbounds: []option.Inbound{
			{
				Type: C.TypeMixed,
				Tag:  "mixed-in",
				Options: &option.HTTPMixedInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
						ListenPort: clientPort,
					},
				},
			},
			{
				Type: C.TypeNaive,
				Tag:  "naive-in",
				Options: &option.NaiveInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
						ListenPort: serverPort,
					},
					Users: []auth.User{
						{
							Username: "sekai",
							Password: "password",
						},
					},
					Network: network.NetworkTCP,
					InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
						TLS: &option.InboundTLSOptions{
							Enabled:         true,
							ServerName:      "example.org",
							CertificatePath: certPem,
							KeyPath:         keyPem,
						},
					},
				},
			},
		},
		Outbounds: []option.Outbound{
			{
				Type: C.TypeDirect,
			},
			{
				Type: C.TypeNaive,
				Tag:  "naive-out",
				Options: &option.NaiveOutboundOptions{
					ServerOptions: option.ServerOptions{
						Server:     "127.0.0.1",
						ServerPort: serverPort,
					},
					Username:            "sekai",
					Password:            "password",
					InsecureConcurrency: 3,
					OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
						TLS: &option.OutboundTLSOptions{
							Enabled:     true,
							ServerName:  "example.org",
							Certificate: []string{string(caPemContent)},
						},
					},
				},
			},
		},
		Route: &option.RouteOptions{
			Rules: []option.Rule{
				{
					Type: C.RuleTypeDefault,
					DefaultOptions: option.DefaultRule{
						RawDefaultRule: option.RawDefaultRule{
							Inbound: []string{"mixed-in"},
						},
						RuleAction: option.RuleAction{
							Action: C.RuleActionTypeRoute,
							RouteOptions: option.RouteActionOptions{
								Outbound: "naive-out",
							},
						},
					},
				},
			},
		},
	})

	naiveOut, ok := instance.Outbound().Outbound("naive-out")
	require.True(t, ok)
	naiveOutbound := naiveOut.(*naive.Outbound)

	netLogPath := "/tmp/naive_concurrency_netlog.json"
	require.True(t, naiveOutbound.Client().Engine().StartNetLogToFile(netLogPath, true))
	defer naiveOutbound.Client().Engine().StopNetLog()

	// Send multiple sequential connections to trigger round-robin
	// With insecure_concurrency=3, connections will be distributed to 3 pools
	for i := 0; i < 6; i++ {
		testTCP(t, clientPort, testPort)
	}

	naiveOutbound.Client().Engine().StopNetLog()

	// Verify NetLog contains multiple independent HTTP/2 sessions
	logContent, err := os.ReadFile(netLogPath)
	require.NoError(t, err)
	var netLog struct {
		Constants struct {
			LogEventTypes map[string]int `json:"logEventTypes"`
		} `json:"constants"`
		Events []struct {
			Type int `json:"type"`
		} `json:"events"`
	}
	require.NoError(t, json.Unmarshal(logContent, &netLog))
	// Chromium assigns new numeric IDs as its NetLog event list evolves.
	sessionType, ok := netLog.Constants.LogEventTypes["HTTP2_SESSION_INITIALIZED"]
	require.True(t, ok, "HTTP2_SESSION_INITIALIZED not in NetLog constants")
	var sessionCount int
	for _, event := range netLog.Events {
		if event.Type == sessionType {
			sessionCount++
		}
	}
	require.GreaterOrEqual(t, sessionCount, 3,
		"Expected at least 3 HTTP/2 sessions with insecure_concurrency=3. NetLog: %s", netLogPath)
}

func TestNaiveSelfQUIC(t *testing.T) {
	caPem, certPem, keyPem := createSelfSignedCertificate(t, "example.org")
	caPemContent, err := os.ReadFile(caPem)
	require.NoError(t, err)
	startInstance(t, option.Options{
		Inbounds: []option.Inbound{
			{
				Type: C.TypeMixed,
				Tag:  "mixed-in",
				Options: &option.HTTPMixedInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
						ListenPort: clientPort,
					},
				},
			},
			{
				Type: C.TypeNaive,
				Tag:  "naive-in",
				Options: &option.NaiveInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
						ListenPort: serverPort,
					},
					Users: []auth.User{
						{
							Username: "sekai",
							Password: "password",
						},
					},
					Network: network.NetworkUDP,
					InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
						TLS: &option.InboundTLSOptions{
							Enabled:         true,
							ServerName:      "example.org",
							CertificatePath: certPem,
							KeyPath:         keyPem,
						},
					},
				},
			},
		},
		Outbounds: []option.Outbound{
			{
				Type: C.TypeDirect,
			},
			{
				Type: C.TypeNaive,
				Tag:  "naive-out",
				Options: &option.NaiveOutboundOptions{
					ServerOptions: option.ServerOptions{
						Server:     "127.0.0.1",
						ServerPort: serverPort,
					},
					Username: "sekai",
					Password: "password",
					QUIC:     true,
					OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
						TLS: &option.OutboundTLSOptions{
							Enabled:     true,
							ServerName:  "example.org",
							Certificate: []string{string(caPemContent)},
						},
					},
				},
			},
		},
		Route: &option.RouteOptions{
			Rules: []option.Rule{
				{
					Type: C.RuleTypeDefault,
					DefaultOptions: option.DefaultRule{
						RawDefaultRule: option.RawDefaultRule{
							Inbound: []string{"mixed-in"},
						},
						RuleAction: option.RuleAction{
							Action: C.RuleActionTypeRoute,
							RouteOptions: option.RouteActionOptions{
								Outbound: "naive-out",
							},
						},
					},
				},
			},
		},
	})
	testTCP(t, clientPort, testPort)
}

func TestNaiveSelfQUICCongestionControl(t *testing.T) {
	testCases := []struct {
		name              string
		congestionControl string
	}{
		{"BBR", "bbr"},
		{"BBR2", "bbr2"},
		{"Cubic", "cubic"},
		{"Reno", "reno"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			caPem, certPem, keyPem := createSelfSignedCertificate(t, "example.org")
			caPemContent, err := os.ReadFile(caPem)
			require.NoError(t, err)
			serverCongestionControl := tc.congestionControl
			if serverCongestionControl == "bbr2" {
				// BBRv2 is a Cronet client option; the quic-go server supports
				// bbr/cubic/reno and need not use the client's algorithm.
				serverCongestionControl = "bbr"
			}
			startInstance(t, option.Options{
				Inbounds: []option.Inbound{
					{
						Type: C.TypeMixed,
						Tag:  "mixed-in",
						Options: &option.HTTPMixedInboundOptions{
							ListenOptions: option.ListenOptions{
								Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
								ListenPort: clientPort,
							},
						},
					},
					{
						Type: C.TypeNaive,
						Tag:  "naive-in",
						Options: &option.NaiveInboundOptions{
							ListenOptions: option.ListenOptions{
								Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
								ListenPort: serverPort,
							},
							Users: []auth.User{
								{
									Username: "sekai",
									Password: "password",
								},
							},
							Network:               network.NetworkUDP,
							QUICCongestionControl: serverCongestionControl,
							InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
								TLS: &option.InboundTLSOptions{
									Enabled:         true,
									ServerName:      "example.org",
									CertificatePath: certPem,
									KeyPath:         keyPem,
								},
							},
						},
					},
				},
				Outbounds: []option.Outbound{
					{
						Type: C.TypeDirect,
					},
					{
						Type: C.TypeNaive,
						Tag:  "naive-out",
						Options: &option.NaiveOutboundOptions{
							ServerOptions: option.ServerOptions{
								Server:     "127.0.0.1",
								ServerPort: serverPort,
							},
							Username:              "sekai",
							Password:              "password",
							QUIC:                  true,
							QUICCongestionControl: tc.congestionControl,
							OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
								TLS: &option.OutboundTLSOptions{
									Enabled:     true,
									ServerName:  "example.org",
									Certificate: []string{string(caPemContent)},
								},
							},
						},
					},
				},
				Route: &option.RouteOptions{
					Rules: []option.Rule{
						{
							Type: C.RuleTypeDefault,
							DefaultOptions: option.DefaultRule{
								RawDefaultRule: option.RawDefaultRule{
									Inbound: []string{"mixed-in"},
								},
								RuleAction: option.RuleAction{
									Action: C.RuleActionTypeRoute,
									RouteOptions: option.RouteActionOptions{
										Outbound: "naive-out",
									},
								},
							},
						},
					},
				},
			})
			testTCP(t, clientPort, testPort)
		})
	}
}
