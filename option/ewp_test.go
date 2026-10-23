package option

import (
	"context"
	"testing"

	"github.com/sagernet/sing/common/json"
	"github.com/stretchr/testify/require"
)

func TestEWPEarlyServerFinishedJSON(t *testing.T) {
	var inbound EWPInboundOptions
	require.NoError(t, json.UnmarshalContext(context.Background(), []byte(`{"early_server_finished":true}`), &inbound))
	require.True(t, inbound.EarlyServerFinished)

	var outbound EWPOutboundOptions
	require.NoError(t, json.UnmarshalContext(context.Background(), []byte(`{"early_server_finished":true}`), &outbound))
	require.True(t, outbound.EarlyServerFinished)
}
